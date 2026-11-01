//go:build ignore

package main

import (
	"errors"
	"sync"
)

const (
	vendorUsagePage = 0x0A
	cmdReportLen    = 32
	pidWired        = 0x026E
	pidDongle       = 0x026F
	pidBLE          = 0x026D
	appVersion      = "test"
	projectURL      = "https://example.invalid"
)

type hidDevice struct {
	Path               string
	VID, PID           uint16
	UsagePage, Usage   uint16
	InLen, FeatLen     uint16
}
type statusReport struct {
	Path string
	PID  uint16
	Data []byte
}
type osdContent struct {
	Caption, Value string
	Dots, Active   int
	Accent         uint32
	Style          PopupStyle
}

var (
	logEnabled bool
	sentMu     sync.Mutex
	sent       [][]byte
	ackFail    bool
	procs      = map[string]int{}
	osdShown   []osdContent
)

func logf(string, ...any)             {}
func osdShow(c osdContent)            { osdShown = append(osdShown, c) }
func runningProcesses() map[string]int { return procs }
func autostartEnabled() bool           { return false }
func setAutostart(bool)                {}

var devices []hidDevice

func enumerateHID(bool) []hidDevice { return devices }
func isCommandCollection(d hidDevice) bool {
	return d.UsagePage == vendorUsagePage && d.FeatLen >= cmdReportLen
}

type cmdChannel struct{}

func openCmdChannel(hidDevice) (*cmdChannel, error) { return &cmdChannel{}, nil }
func (c *cmdChannel) close()                      {}
func (c *cmdChannel) send(p []byte) error {
	if ackFail {
		return errors.New("no ack")
	}
	sentMu.Lock()
	sent = append(sent, append([]byte(nil), p...))
	sentMu.Unlock()
	return nil
}

func dumpFlash(dir string) (string, error) { return "", errors.New("not on linux") }
func shellOpen(string)                      {}

func readMouseMemory() (*MouseMemory, error) { return nil, errors.New("not on linux") }
