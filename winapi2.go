//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	pHidD_SetFeature = hid.NewProc("HidD_SetFeature")
	pHidD_GetFeature = hid.NewProc("HidD_GetFeature")

	pCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	pProcess32NextW           = kernel32.NewProc("Process32NextW")

	pFindWindowW        = user32.NewProc("FindWindowW")
	pIsIconic           = user32.NewProc("IsIconic")
	pAllowSetForeground = user32.NewProc("AllowSetForegroundWindow")
	pInsertMenuW        = user32.NewProc("AppendMenuW")
)

const (
	TH32CS_SNAPPROCESS = 0x2
	MF_POPUP           = 0x0010
	SW_RESTORE         = 9
)

type PROCESSENTRY32W struct {
	DwSize              uint32
	CntUsage            uint32
	Th32ProcessID       uint32
	Th32DefaultHeapID   uintptr
	Th32ModuleID        uint32
	CntThreads          uint32
	Th32ParentProcessID uint32
	PcPriClassBase      int32
	DwFlags             uint32
	SzExeFile           [260]uint16
}

// runningProcesses returns the lower-case executable names of all processes.
func runningProcesses() map[string]int {
	out := map[string]int{}
	h, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if h == INVALID_HANDLE {
		return out
	}
	defer pCloseHandle.Call(h)
	var pe PROCESSENTRY32W
	pe.DwSize = uint32(unsafe.Sizeof(pe))
	r, _, _ := pProcess32FirstW.Call(h, ptr(&pe))
	for r != 0 {
		name := lower(syscall.UTF16ToString(pe.SzExeFile[:]))
		out[name]++
		r, _, _ = pProcess32NextW.Call(h, ptr(&pe))
	}
	return out
}

var pGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

// systemLanguage: Russian UI language → "ru", anything else → "en".
func systemLanguage() string {
	id, _, _ := pGetUserDefaultUILanguage.Call()
	if id&0x3FF == 0x19 {
		return "ru"
	}
	return "en"
}
