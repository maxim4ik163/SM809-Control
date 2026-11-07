//go:build windows

package main

// Autostart.
//
// Normally: a value in HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
// Windows silently skips Run entries that need elevation (the exe has
// "Run as administrator" set in its compatibility properties), so an
// elevated SM809 Control uses a Task Scheduler task instead: started at
// logon "with highest privileges", no UAC prompt.

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "SM809Control"
const taskName = "SM809Control"

var (
	pOpenProcessToken    = advapi32.NewProc("OpenProcessToken")
	pGetTokenInformation = advapi32.NewProc("GetTokenInformation")
	pGetCurrentProcess   = kernel32.NewProc("GetCurrentProcess")
)

var elevatedOnce = sync.OnceValue(func() bool {
	const tokenQuery, tokenElevation = 0x0008, 20
	proc, _, _ := pGetCurrentProcess.Call()
	var tok uintptr
	if r, _, _ := pOpenProcessToken.Call(proc, tokenQuery, ptr(&tok)); r == 0 {
		return false
	}
	defer pCloseHandle.Call(tok)
	var elev, n uint32
	if r, _, _ := pGetTokenInformation.Call(tok, tokenElevation, ptr(&elev), 4, ptr(&n)); r == 0 {
		return false
	}
	return elev != 0
})

// isElevated: this process runs with administrator rights.
func isElevated() bool { return elevatedOnce() }

// ---- Run value ----

func runValueExists() bool {
	var k uintptr
	if r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, uintptr(ptrU16(runKey)), 0, KEY_READ, ptr(&k)); r != 0 {
		return false
	}
	defer pRegCloseKey.Call(k)
	r, _, _ := pRegQueryValueExW.Call(k, uintptr(ptrU16(runName)), 0, 0, 0, 0)
	return r == 0
}

func setRunValue(on bool) error {
	var k uintptr
	if r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, uintptr(ptrU16(runKey)), 0, KEY_WRITE, ptr(&k)); r != 0 {
		return syscall.Errno(r)
	}
	defer pRegCloseKey.Call(k)
	if !on {
		if r, _, _ := pRegDeleteValueW.Call(k, uintptr(ptrU16(runName))); r != 0 && r != uintptr(syscall.ERROR_FILE_NOT_FOUND) {
			return syscall.Errno(r)
		}
		return nil
	}
	exe, _ := os.Executable()
	v, _ := syscall.UTF16FromString(`"` + exe + `" --tray`)
	if r, _, _ := pRegSetValueExW.Call(k, uintptr(ptrU16(runName)), 0, REG_SZ, uintptr(unsafePtr(&v[0])), uintptr(len(v)*2)); r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// ---- scheduler task (schtasks.exe) ----

var taskCache struct {
	sync.Mutex
	exists bool
	at     time.Time
}

func schtasks(args ...string) (string, error) {
	c := exec.Command("schtasks.exe", args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// taskExists asks schtasks at most every 10 s (the tray menu and the UI ask often).
func taskExists() bool {
	taskCache.Lock()
	defer taskCache.Unlock()
	if !taskCache.at.IsZero() && time.Since(taskCache.at) < 10*time.Second {
		return taskCache.exists
	}
	_, err := schtasks("/Query", "/TN", taskName)
	taskCache.exists, taskCache.at = err == nil, time.Now()
	return taskCache.exists
}

func setTaskCache(exists bool) {
	taskCache.Lock()
	taskCache.exists, taskCache.at = exists, time.Now()
	taskCache.Unlock()
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

// taskXML: logon trigger for the current user, highest privileges, and the
// settings the scheduler's defaults get wrong for a tray app: no 72-hour time
// limit, runs on battery, normal priority (default 7 = below normal).
func taskXML(exe, userID string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>SM809 Control: start with Windows as administrator</Description></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled><UserId>` + xmlEsc(userID) + `</UserId><Delay>PT5S</Delay></LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author"><UserId>` + xmlEsc(userID) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <IdleSettings><StopOnIdleEnd>false</StopOnIdleEnd><RestartOnIdle>false</RestartOnIdle></IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>4</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>` + xmlEsc(exe) + `</Command><Arguments>--tray</Arguments><WorkingDirectory>` + xmlEsc(filepath.Dir(exe)) + `</WorkingDirectory></Exec>
  </Actions>
</Task>
`
}

func createTask() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	// schtasks reads the XML as UTF-16 (with BOM)
	w := utf16.Encode([]rune(taskXML(exe, u.Uid))) // Uid = SID on Windows
	b := make([]byte, 2+2*len(w))
	b[0], b[1] = 0xFF, 0xFE
	for i, c := range w {
		b[2+2*i], b[3+2*i] = byte(c), byte(c>>8)
	}
	f, err := os.CreateTemp("", "sm809task-*.xml")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if out, err := schtasks("/Create", "/TN", taskName, "/XML", name, "/F"); err != nil {
		logf("schtasks /Create: %v %s", err, out)
		return errors.New(T("Планировщик заданий: ", "Task Scheduler: ") + firstLine(out, err))
	}
	setTaskCache(true)
	return nil
}

func deleteTask() error {
	if !taskExists() {
		return nil
	}
	if out, err := schtasks("/Delete", "/TN", taskName, "/F"); err != nil {
		logf("schtasks /Delete: %v %s", err, out)
		setTaskCache(true)
		return errors.New(T("Планировщик заданий: ", "Task Scheduler: ") + firstLine(out, err))
	}
	setTaskCache(false)
	return nil
}

// firstLine: schtasks prints in the console code page, so only the exit code is shown (the text goes to the log).
func firstLine(_ string, err error) string { return err.Error() }

// ---- public ----

func autostartEnabled() bool { return autostartMode() != "" }

// autostartMode: "task" (scheduler, as administrator), "run" (Run key) or "".
func autostartMode() string {
	if taskExists() {
		return "task"
	}
	if runValueExists() {
		return "run"
	}
	return ""
}

func setAutostart(on bool) error {
	if isElevated() {
		if on {
			if err := createTask(); err != nil {
				return err
			}
			return setRunValue(false) // the Run entry would be skipped anyway
		}
		err := deleteTask()
		if e := setRunValue(false); err == nil {
			err = e
		}
		return err
	}
	if on {
		if taskExists() {
			return nil // already starts as administrator; changing it needs rights
		}
		return setRunValue(true)
	}
	err := setRunValue(false)
	if taskExists() {
		if e := deleteTask(); e != nil {
			return errors.New(T("Автозапуск настроен через Планировщик заданий с правами администратора. Чтобы выключить его, запустите SM809 Control от имени администратора.",
				"Autostart is set up as a Task Scheduler task with administrator rights. Run SM809 Control as administrator to turn it off."))
		}
	}
	return err
}

// refreshAutostart runs at start-up: rewrites the entry with the current exe
// path (the exe may have moved; old Run values lacked --tray) and, when
// elevated, moves a Run entry over to the scheduler.
func refreshAutostart() {
	go func() {
		if isElevated() {
			if runValueExists() || taskExists() {
				if err := setAutostart(true); err != nil {
					logf("автозапуск: %v", err)
				}
			}
			return
		}
		if runValueExists() && !taskExists() {
			_ = setRunValue(true)
		}
	}()
}
