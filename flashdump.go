//go:build windows

package main

// Read-only dump of the mouse's settings flash through the IAP feature report
// 0xBF (the same channel the vendor firmware updater uses).
//
// Firmware handler (USB SET_REPORT, report 0xBF, 136 bytes incl. ID):
//   [0] 0xBF  [1] write length  [2] read length  [3] command  [4] -
//   [5..7] flash address, big endian   [8..] data
// If [1] != 0 the firmware WRITES [1] bytes of data to flash; if [1] == 0 and
// [2] != 0 it READS [2] bytes into [8..] (command byte is not looked at);
// only when both are 0 is [3] executed (1/3 = erase, 2 = enter bootloader).
// GET_REPORT 0xBF then returns the buffer. We therefore always send
// [1] = 0 and [2] = 1..128 and refuse anything else.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

const (
	iapReportID  = 0xBF
	iapReportLen = 136
	iapChunk     = 128
)

// Only the three sectors the firmware uses for settings, keys and macros.
// (Bluetooth pairing data lives elsewhere and is deliberately not read.)
var dumpRegions = []struct {
	Name string
	Addr uint32
	Size uint32
}{
	{"keys", 0x7B000, 0x1000},
	{"config", 0x7C000, 0x1000},
	{"macros", 0x7D000, 0x1000},
}

func iapReadPacket(addr uint32, n int) ([]byte, error) {
	if n < 1 || n > iapChunk {
		return nil, fmt.Errorf("bad read length %d", n)
	}
	b := make([]byte, iapReportLen)
	b[0] = iapReportID
	b[1] = 0 // write length — MUST stay 0
	b[2] = byte(n)
	b[3] = 0 // command — never used when [2] != 0, keep 0 anyway
	b[5], b[6], b[7] = byte(addr>>16), byte(addr>>8), byte(addr)
	// paranoia: never let a packet with a write length or a command out
	if b[1] != 0 || b[3] != 0 || b[2] == 0 {
		panic("unsafe IAP packet")
	}
	return b, nil
}

type iapChannel struct {
	h uintptr
	d hidDevice
}

func (c *iapChannel) read(addr uint32, n int) ([]byte, error) {
	pkt, err := iapReadPacket(addr, n)
	if err != nil {
		return nil, err
	}
	if r, _, e := pHidD_SetFeature.Call(c.h, uintptr(unsafe.Pointer(&pkt[0])), uintptr(len(pkt))); r == 0 {
		return nil, fmt.Errorf("SetFeature: %v", e)
	}
	time.Sleep(5 * time.Millisecond)
	resp := make([]byte, iapReportLen)
	resp[0] = iapReportID
	if r, _, e := pHidD_GetFeature.Call(c.h, uintptr(unsafe.Pointer(&resp[0])), uintptr(len(resp))); r == 0 {
		return nil, fmt.Errorf("GetFeature: %v", e)
	}
	// the header we sent must come back unchanged, otherwise this is not the
	// handler we think it is
	if !bytes.Equal(resp[:8], append([]byte{iapReportID}, pkt[1:8]...)) {
		return nil, fmt.Errorf("unexpected reply header % X", resp[:8])
	}
	return resp[8 : 8+n], nil
}

func findIAPChannel() (*iapChannel, error) {
	var sawDongle bool
	for _, d := range enumerateHID(false) {
		if d.PID == pidDongle {
			sawDongle = true
		}
		if d.PID != pidWired || d.FeatLen < iapReportLen {
			continue
		}
		h := openHID(d.Path, GENERIC_READ|GENERIC_WRITE)
		if h == INVALID_HANDLE {
			continue
		}
		c := &iapChannel{h: h, d: d}
		if _, err := c.read(0x7C000, 16); err == nil {
			return c, nil
		}
		pCloseHandle.Call(h)
	}
	if sawDongle {
		return nil, errors.New(T("Подключите мышь кабелем: чтение памяти работает только по проводу (через донгл ответил бы сам донгл).",
			"Connect the mouse with the cable: memory reading only works wired (through the dongle the dongle itself would answer)."))
	}
	return nil, errors.New(T("Мышь, подключённая кабелем, не найдена или не поддерживает чтение памяти.",
		"No mouse connected by cable was found, or it does not support memory reading."))
}

// dumpFlash reads every region twice and checks both passes are identical.
func dumpFlash(dir string) (string, error) {
	c, err := findIAPChannel()
	if err != nil {
		return "", err
	}
	defer pCloseHandle.Call(c.h)

	readRegion := func(addr, size uint32) ([]byte, error) {
		out := make([]byte, 0, size)
		for off := uint32(0); off < size; off += iapChunk {
			n := iapChunk
			if size-off < iapChunk {
				n = int(size - off)
			}
			b, err := c.read(addr+off, n)
			if err != nil {
				return nil, fmt.Errorf("0x%X: %v", addr+off, err)
			}
			out = append(out, b...)
		}
		return out, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	var rep strings.Builder
	fmt.Fprintf(&rep, "SM809 flash dump %s, app %s\r\nHID %s\r\n\r\n", time.Now().Format(time.RFC3339), appVersion, c.d.Path)
	for _, r := range dumpRegions {
		a, err := readRegion(r.Addr, r.Size)
		if err != nil {
			return "", err
		}
		b, err := readRegion(r.Addr, r.Size)
		if err != nil {
			return "", err
		}
		same := bytes.Equal(a, b)
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s_%05X.bin", r.Name, r.Addr)), a, 0644)
		used := 0
		for _, x := range a {
			if x != 0xFF {
				used++
			}
		}
		fmt.Fprintf(&rep, "== %s 0x%05X, %d bytes, non-FF %d, two passes identical: %v\r\n", r.Name, r.Addr, len(a), used, same)
		// hex dump without trailing erased area
		last := len(a)
		for last > 0 && a[last-1] == 0xFF {
			last--
		}
		last = (last + 15) &^ 15
		rep.WriteString(hex.Dump(a[:last]))
		rep.WriteString("\r\n")
		if !same {
			return "", fmt.Errorf("%s: %s", r.Name, T("два чтения дали разные данные", "two reads differ"))
		}
	}
	// what the app believes is on the mouse, for decoding
	store.mu.Lock()
	cur := store.profile(store.state.LastProfile)
	if cur == nil {
		cur = store.profile(store.data.Settings.BaseProfile)
	}
	if b, err := jsonIndent(struct {
		Profile *Profile
		Macros  []*Macro
		Device  *DeviceState
	}{cur, store.data.Macros, store.state}); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "app_state.json"), b, 0644)
	}
	store.mu.Unlock()
	if err := os.WriteFile(filepath.Join(dir, "dump.txt"), []byte(rep.String()), 0644); err != nil {
		return "", err
	}
	return dir, nil
}

func jsonIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// readMouseMemory reads the config struct and the macro slots of buttons that
// have a macro assigned. Every block is read twice and must match.
func readMouseMemory() (*MouseMemory, error) {
	c, err := findIAPChannel()
	if err != nil {
		return nil, err
	}
	defer pCloseHandle.Call(c.h)
	read := func(addr uint32, size int) ([]byte, error) {
		var out []byte
		for pass := 0; pass < 2; pass++ {
			var buf []byte
			for off := 0; off < size; off += iapChunk {
				n := min(iapChunk, size-off)
				b, err := c.read(addr+uint32(off), n)
				if err != nil {
					return nil, err
				}
				buf = append(buf, b...)
			}
			if pass == 1 && !bytes.Equal(out, buf) {
				return nil, errors.New(T("два чтения памяти мыши не совпали", "two reads of the mouse memory differ"))
			}
			out = buf
		}
		return out, nil
	}
	cfg, err := read(cfgAddr, cfgLen)
	if err != nil {
		return nil, err
	}
	mem := &MouseMemory{Config: cfg, Macros: map[int][]byte{}}
	for k, idx := range dualPacketOrder {
		if cfg[cfgKeys+3*k] != 144 {
			continue
		}
		raw, err := read(macroSlotAddr(idx+1), macroSlotLen)
		if err != nil {
			return nil, err
		}
		mem.Macros[idx+1] = raw
	}
	return mem, nil
}
