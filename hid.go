//go:build windows

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	vendorID        = 0x04F3
	vendorUsagePage = 0x000A
	statusReportID  = 0x04
	statusReportLen = 32
	cmdReportID     = 0x06
	cmdReportLen    = 32 // report 6: ID + 31 data bytes
)

// Product IDs (firmware / vendor tools): wired, 2.4G dongle, Bluetooth LE.
const (
	pidWired  = 0x026E
	pidDongle = 0x026F
	pidBLE    = 0x026D
)

type hidDevice struct {
	Path      string
	VID, PID  uint16
	UsagePage uint16
	Usage     uint16
	InLen     uint16
	FeatLen   uint16
}

func hidGUID() GUID {
	var g GUID
	pHidD_GetHidGuid.Call(ptr(&g))
	return g
}

// enumerateHID lists present HID collections of our vendor.
func enumerateHID(anyVendor bool) []hidDevice {
	g := hidGUID()
	hdev, _, _ := pSetupDiGetClassDevsW.Call(ptr(&g), 0, 0, DIGCF_PRESENT|DIGCF_DEVICEINTERFACE)
	if hdev == INVALID_HANDLE || hdev == 0 {
		return nil
	}
	defer pSetupDiDestroyDeviceInfoList.Call(hdev)
	var out []hidDevice
	for i := uintptr(0); ; i++ {
		var ifd SP_DEVICE_INTERFACE_DATA
		ifd.CbSize = uint32(unsafe.Sizeof(ifd))
		if r, _, _ := pSetupDiEnumDeviceInterfaces.Call(hdev, 0, ptr(&g), i, ptr(&ifd)); r == 0 {
			break
		}
		var need uint32
		pSetupDiGetDeviceInterfaceDetailW.Call(hdev, ptr(&ifd), 0, 0, ptr(&need), 0)
		if need == 0 || need > 4096 {
			continue
		}
		buf := make([]byte, need+8)
		if unsafe.Sizeof(uintptr(0)) == 8 {
			*(*uint32)(unsafe.Pointer(&buf[0])) = 8
		} else {
			*(*uint32)(unsafe.Pointer(&buf[0])) = 6
		}
		if r, _, _ := pSetupDiGetDeviceInterfaceDetailW.Call(hdev, ptr(&ifd), uintptr(unsafe.Pointer(&buf[0])), uintptr(need), 0, 0); r == 0 {
			continue
		}
		path := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), (len(buf)-4)/2))
		if d, ok := queryHID(path); ok && (anyVendor || d.VID == vendorID) {
			out = append(out, d)
		}
	}
	return out
}

func openHID(path string, access uint32) uintptr {
	h, _, _ := pCreateFileW.Call(uintptr(unsafe.Pointer(u16(path))), uintptr(access),
		FILE_SHARE_READ|FILE_SHARE_WRITE, 0, OPEN_EXISTING, 0, 0)
	return h
}

func queryHID(path string) (hidDevice, bool) {
	d := hidDevice{Path: path}
	h := openHID(path, 0)
	if h == INVALID_HANDLE {
		return d, false
	}
	defer pCloseHandle.Call(h)
	var a HIDD_ATTRIBUTES
	a.Size = uint32(unsafe.Sizeof(a))
	if r, _, _ := pHidD_GetAttributes.Call(h, ptr(&a)); r == 0 {
		return d, false
	}
	d.VID, d.PID = a.VendorID, a.ProductID
	var pp uintptr
	if r, _, _ := pHidD_GetPreparsedData.Call(h, ptr(&pp)); r == 0 {
		return d, true
	}
	defer pHidD_FreePreparsedData.Call(pp)
	var caps [64]byte
	if st, _, _ := pHidP_GetCaps.Call(pp, uintptr(unsafe.Pointer(&caps[0]))); uint32(st) == HIDP_STATUS_SUCCESS {
		d.Usage = *(*uint16)(unsafe.Pointer(&caps[0]))
		d.UsagePage = *(*uint16)(unsafe.Pointer(&caps[2]))
		d.InLen = *(*uint16)(unsafe.Pointer(&caps[4]))
		d.FeatLen = *(*uint16)(unsafe.Pointer(&caps[8]))
	}
	return d, true
}

// isStatusCollection: the vendor collection with input report 4. Over
// Bluetooth Windows may report another input length than the 32 bytes of
// USB, so any vendor collection with an input report of 10+ bytes is read.
func isStatusCollection(d hidDevice) bool {
	return d.UsagePage == vendorUsagePage && d.InLen >= 10
}

// readerStats is shown by Settings → Mouse → connection diagnostics.
type readerStats struct {
	Opened    bool      `json:"opened"`
	Reports   int       `json:"reports"`   // status reports received (pushed or polled)
	Polled    int       `json:"polled"`    // of those, read by polling
	PollFails int       `json:"pollFails"` // HidD_GetInputReport failures
	Feat5     int       `json:"feat5"`     // feature report 5 reads that looked like a status
	Feat5Bad  int       `json:"feat5Bad"`  // … that did not (or failed)
	Last      time.Time `json:"last"`
	Error     string    `json:"error,omitempty"`
	LastInput string    `json:"lastInput,omitempty"` // hex of the last report 4
	LastFeat5 string    `json:"lastFeat5,omitempty"` // hex of the last feature report 5
}

// commandsActive is set while the engine writes the mouse, so polling does
// not interleave with a command sequence.
var commandsActive atomic.Bool

var (
	statsMu sync.Mutex
	stats   = map[string]*readerStats{}
)

func readerStat(path string) *readerStats {
	statsMu.Lock()
	defer statsMu.Unlock()
	s := stats[path]
	if s == nil {
		s = &readerStats{}
		stats[path] = s
	}
	return s
}

func statsSnapshot() map[string]readerStats {
	statsMu.Lock()
	defer statsMu.Unlock()
	out := map[string]readerStats{}
	for k, v := range stats {
		out[k] = *v
	}
	return out
}

func isCommandCollection(d hidDevice) bool {
	return d.UsagePage == vendorUsagePage && d.FeatLen >= cmdReportLen
}

// ---------------------------------------------------------------------------
// Feature-report command channel

type cmdChannel struct {
	dev hidDevice
	h   uintptr
}

type noAckError struct{}

func (noAckError) Error() string {
	return T("мышь не подтвердила команду", "the mouse did not acknowledge the command")
}

var errNoAck error = noAckError{}

func openCmdChannel(d hidDevice) (*cmdChannel, error) {
	h := openHID(d.Path, GENERIC_READ|GENERIC_WRITE)
	if h == INVALID_HANDLE {
		return nil, fmt.Errorf(T("не удалось открыть канал команд %04X:%04X", "cannot open the command interface %04X:%04X"), d.VID, d.PID)
	}
	return &cmdChannel{dev: d, h: h}, nil
}

func (c *cmdChannel) close() {
	if c != nil && c.h != INVALID_HANDLE && c.h != 0 {
		pCloseHandle.Call(c.h)
		c.h = 0
	}
}

// setFeature sends one 31-byte payload (without report ID) as report 0x06.
// Windows accepts the short 32-byte buffer (that is what the vendor app does);
// if a driver insists on the collection's full feature length we retry with it.
func (c *cmdChannel) setFeature(payload []byte) error {
	for _, n := range []int{cmdReportLen, int(c.dev.FeatLen)} {
		if n < cmdReportLen {
			continue
		}
		buf := make([]byte, n)
		buf[0] = cmdReportID
		copy(buf[1:], payload) // macro packets are 32 bytes; the last one is always 0 and is dropped here
		if r, _, _ := pHidD_SetFeature.Call(c.h, uintptr(unsafe.Pointer(&buf[0])), uintptr(n)); r != 0 {
			return nil
		}
	}
	return errors.New("HidD_SetFeature failed")
}

func (c *cmdChannel) getAck() (bool, error) {
	for _, n := range []int{cmdReportLen, int(c.dev.FeatLen)} {
		if n < cmdReportLen {
			continue
		}
		buf := make([]byte, n)
		buf[0] = cmdReportID
		if r, _, _ := pHidD_GetFeature.Call(c.h, uintptr(unsafe.Pointer(&buf[0])), uintptr(n)); r != 0 {
			return buf[1] == 1, nil
		}
	}
	return false, errors.New("HidD_GetFeature failed")
}

// send mirrors the web driver's sendCommand(): up to 3 attempts, 100+50*i ms
// pause before each, ack read 60 ms later. The same packet (same seq) is
// resent on retry.
func (c *cmdChannel) send(payload []byte) error {
	var last error
	for i := 0; i < 3; i++ {
		time.Sleep(time.Duration(100+50*i) * time.Millisecond)
		if err := c.setFeature(payload); err != nil {
			last = err
		}
		time.Sleep(60 * time.Millisecond)
		ok, err := c.getAck()
		if err != nil {
			last = err
			continue
		}
		if ok {
			return nil
		}
		last = errNoAck
	}
	return last
}

// ---------------------------------------------------------------------------
// Status readers

type statusReport struct {
	Path string
	PID  uint16
	Data []byte // includes report ID at [0]
}

var (
	openMu    sync.Mutex
	openPaths = map[string]bool{}
)

func startStatusReader(d hidDevice, onReport func(statusReport), onGone func(string)) bool {
	openMu.Lock()
	if openPaths[d.Path] {
		openMu.Unlock()
		return false
	}
	openPaths[d.Path] = true
	openMu.Unlock()
	st := readerStat(d.Path)
	report := func(buf []byte, polled bool) {
		statsMu.Lock()
		st.Reports++
		if polled {
			st.Polled++
		}
		st.Last = time.Now()
		if buf[0] == statusReportID {
			st.LastInput = hex.EncodeToString(buf[:min(len(buf), 16)])
		}
		statsMu.Unlock()
		onReport(statusReport{Path: d.Path, PID: d.PID, Data: append([]byte(nil), buf...)})
	}
	go func() {
		defer func() {
			openMu.Lock()
			delete(openPaths, d.Path)
			openMu.Unlock()
			statsMu.Lock()
			st.Opened = false
			statsMu.Unlock()
			onGone(d.Path)
		}()
		h := openHID(d.Path, GENERIC_READ|GENERIC_WRITE)
		if h == INVALID_HANDLE {
			h = openHID(d.Path, GENERIC_READ)
		}
		if h == INVALID_HANDLE {
			logf("не удалось открыть %04X:%04X для чтения статуса", d.VID, d.PID)
			statsMu.Lock()
			st.Error = "open failed"
			statsMu.Unlock()
			return
		}
		defer pCloseHandle.Call(h)
		statsMu.Lock()
		st.Opened, st.Error = true, ""
		statsMu.Unlock()
		logf("статус: подключено %04X:%04X in=%d", d.VID, d.PID, d.InLen)
		n := int(d.InLen)
		if n < statusReportLen {
			n = statusReportLen
		}
		done := make(chan struct{})
		defer close(done)
		// Polling on a second handle (a synchronous handle serialises I/O, so
		// it cannot share the one blocked in ReadFile): the mouse pushes report 4
		// over USB and the dongle, but over Bluetooth it may stay silent.
		go pollStatus(d, n, done, func(buf []byte) { report(buf, true) }, st)
		buf := make([]byte, n)
		for {
			var got uint32
			r, _, err := pReadFile.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), ptr(&got), 0)
			if r == 0 {
				logf("статус: отключено %04X:%04X (%v)", d.VID, d.PID, err)
				return
			}
			if got >= 10 && buf[0] == statusReportID {
				report(buf[:got], false)
			}
		}
	}()
	return true
}

// pollStatus reads input report 4 with HidD_GetInputReport right away and
// then whenever nothing arrived for a while.
func pollStatus(d hidDevice, n int, done chan struct{}, onReport func([]byte), st *readerStats) {
	h := openHID(d.Path, GENERIC_READ|GENERIC_WRITE)
	if h == INVALID_HANDLE {
		h = openHID(d.Path, GENERIC_READ)
	}
	if h == INVALID_HANDLE {
		return
	}
	defer pCloseHandle.Call(h)
	// the command collection of the same device, for feature report 5 (the
	// vendor tools poll it; over Bluetooth input report 4 cannot be read)
	var cmd hidDevice
	for _, c := range enumerateHID(false) {
		if c.PID == d.PID && isCommandCollection(c) && samePhysical(c.Path, d.Path) {
			cmd = c
			break
		}
	}
	feat5 := func() bool {
		if cmd.Path == "" || commandsActive.Load() {
			return false
		}
		ch := openHID(cmd.Path, GENERIC_READ|GENERIC_WRITE)
		if ch == INVALID_HANDLE {
			return false
		}
		defer pCloseHandle.Call(ch)
		for _, ln := range []int{cmdReportLen, int(cmd.FeatLen)} {
			if ln < cmdReportLen {
				continue
			}
			buf := make([]byte, ln)
			buf[0] = 0x05
			if r, _, _ := pHidD_GetFeature.Call(ch, uintptr(unsafe.Pointer(&buf[0])), uintptr(ln)); r == 0 {
				continue
			}
			ok := plausibleStatus(buf)
			statsMu.Lock()
			st.LastFeat5 = hex.EncodeToString(buf[:min(len(buf), 16)])
			if ok {
				st.Feat5++
			} else {
				st.Feat5Bad++
			}
			statsMu.Unlock()
			if ok {
				onReport(buf)
			}
			return ok
		}
		statsMu.Lock()
		st.Feat5Bad++
		statsMu.Unlock()
		return false
	}
	inputFails := 0
	poll := func() {
		if feat5() {
			return
		}
		// input report 4: works over USB; over Bluetooth it fails (and may
		// block), so it is given up after two failures in a row
		if inputFails >= 2 {
			return
		}
		buf := make([]byte, n)
		buf[0] = statusReportID
		if r, _, _ := pHidD_GetInputReport.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r != 0 && buf[0] == statusReportID {
			inputFails = 0
			for _, b := range buf[1:10] {
				if b != 0 {
					onReport(buf)
					return
				}
			}
			return
		}
		inputFails++
		statsMu.Lock()
		st.PollFails++
		statsMu.Unlock()
	}
	poll()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			statsMu.Lock()
			silent := time.Since(st.Last) > 8*time.Second
			statsMu.Unlock()
			if silent {
				poll()
			}
		}
	}
}
