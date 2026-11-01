//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const appVersion = "2.1.2"

const projectURL = "https://github.com/maxim4ik163/SM809-Control"

const (
	mainClass   = "SM809ControlMain"
	uiTitle     = "SM809 Control"
	msgTray     = WM_APP + 1
	msgCall     = WM_APP + 2
	msgOpenUI   = WM_APP + 3
	timerRescan = 1
	timerSoon   = 2
	timerProc   = 3

	cmdStatus    = 100
	cmdOpenUI    = 101
	cmdAutostart = 102
	cmdApplyAll  = 103
	cmdExit      = 109
	cmdProfile0  = 1000
)

var (
	hwndMain   uintptr
	hinst      uintptr
	nid        NOTIFYICONDATAW
	curIcon    uintptr
	msgTaskbar uintptr

	store *Store
	eng   *Engine
	api   *API
	soft  *softRuntime

	callMu sync.Mutex
	calls  []func()
)

// uiCall runs f on the UI thread (window procedures, tray, OSD).
func uiCall(f func()) {
	callMu.Lock()
	calls = append(calls, f)
	callMu.Unlock()
	pPostMessageW.Call(hwndMain, msgCall, 0, 0)
}

func modeName(m string) string {
	switch m {
	case "wired":
		return T("провод", "wired")
	case "dongle":
		return T("донгл 2.4G", "2.4G dongle")
	case "bt1":
		return "Bluetooth 1"
	case "bt2":
		return "Bluetooth 2"
	case "":
		return ""
	}
	if strings.HasPrefix(m, "bt") {
		return "Bluetooth " + m[2:]
	}
	return m
}

func tooltipText() string {
	s := eng.Snapshot()
	p := s.Primary
	if p == nil {
		return T("SM809: мышь не найдена", "SM809: mouse not found")
	}
	if !p.Link {
		return T("SM809: мышь не на связи (", "SM809: mouse not connected (") + modeName(p.Mode) + ")"
	}
	parts := []string{fmt.Sprintf("SM809: %d%%", p.Battery)}
	switch p.Charge {
	case 1:
		parts = append(parts, T("заряжается", "charging"))
	case 2:
		parts = append(parts, T("заряжена", "charged"))
	}
	if f := s.Battery; f.Hours > 0 {
		if f.State == "charging" {
			parts = append(parts, T("до полной ", "full in ")+fmtHours(f.Hours))
		} else if f.State == "discharging" {
			parts = append(parts, "≈ "+fmtHours(f.Hours))
		}
	}
	parts = append(parts, modeName(p.Mode))
	store.mu.Lock()
	id := s.Active
	if id == "" {
		id = s.Base
	}
	if pr := store.profile(id); pr != nil {
		parts = append(parts, pr.Name)
	}
	store.mu.Unlock()
	t := strings.Join(parts, " · ")
	if len([]rune(t)) > 120 {
		t = string([]rune(t)[:120])
	}
	return t
}

func updateTray() {
	s := eng.Snapshot()
	pct, chg, full := -1, false, false
	if p := s.Primary; p != nil && p.Link {
		pct, chg, full = p.Battery, p.Charge == 1, p.Charge == 2
	}
	ic := batteryIcon(pct, chg, full)
	nid.UFlags = NIF_ICON | NIF_TIP | NIF_MESSAGE | NIF_SHOWTIP
	if ic != 0 {
		nid.HIcon = ic
	}
	copyU16(nid.SzTip[:], tooltipText())
	pShellNotifyIconW.Call(NIM_MODIFY, ptr(&nid))
	if ic != 0 && curIcon != 0 {
		pDestroyIcon.Call(curIcon)
	}
	if ic != 0 {
		curIcon = ic
	}
}

func notify(title, text string, warn bool) {
	nid.UFlags = NIF_INFO
	copyU16(nid.SzInfoTitle[:], title)
	copyU16(nid.SzInfo[:], text)
	nid.DwInfoFlags = NIIF_INFO
	if warn {
		nid.DwInfoFlags = NIIF_WARNING
	}
	pShellNotifyIconW.Call(NIM_MODIFY, ptr(&nid))
}

func addTray() {
	nid = NOTIFYICONDATAW{}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwndMain
	nid.UID = 1
	nid.UFlags = NIF_ICON | NIF_TIP | NIF_MESSAGE | NIF_SHOWTIP
	nid.UCallbackMessage = msgTray
	curIcon = batteryIcon(-1, false, false)
	nid.HIcon = curIcon
	copyU16(nid.SzTip[:], "SM809 Control")
	pShellNotifyIconW.Call(NIM_ADD, ptr(&nid))
	nid.UVersion = 4
	pShellNotifyIconW.Call(NIM_SETVERSION, ptr(&nid))
	updateTray()
}

var firstScan = true

func rescan() {
	for _, d := range enumerateHID(false) {
		if firstScan {
			logf("HID: %04X:%04X page=%04X usage=%04X in=%d feat=%d %s", d.VID, d.PID, d.UsagePage, d.Usage, d.InLen, d.FeatLen, d.Path)
		}
		if isStatusCollection(d) {
			startStatusReader(d, eng.handleReport, func(path string) {
				eng.deviceGone(path)
				uiCall(func() { pSetTimer.Call(hwndMain, timerSoon, 1500, 0) })
			})
		}
	}
	firstScan = false
}

func showMenu() {
	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	add := func(menu uintptr, flags uint32, id int, text string) {
		pAppendMenuW.Call(menu, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(u16(text))))
	}
	add(m, MF_STRING|MF_GRAYED, cmdStatus, tooltipText())
	add(m, MF_SEPARATOR, 0, "")
	add(m, MF_STRING, cmdOpenUI, T("Открыть SM809 Control", "Open SM809 Control"))

	// profiles submenu
	sub, _, _ := pCreatePopupMenu.Call()
	snap := eng.Snapshot()
	store.mu.Lock()
	var ids []string
	for i, p := range store.data.Profiles {
		fl := uint32(MF_STRING)
		if p.ID == snap.Base {
			fl |= MF_CHECKED
		}
		name := p.Name
		if p.ID == snap.Active && p.ID != snap.Base {
			name += T("  (активен по правилу)", "  (active by rule)")
		}
		add(sub, fl, cmdProfile0+i, name)
		ids = append(ids, p.ID)
	}
	store.mu.Unlock()
	pAppendMenuW.Call(m, MF_POPUP, sub, uintptr(unsafe.Pointer(u16(T("Профиль", "Profile")))))
	add(m, MF_STRING, cmdApplyAll, T("Записать текущий профиль в мышь полностью", "Write the current profile to the mouse in full"))

	add(m, MF_SEPARATOR, 0, "")
	as := uint32(MF_STRING)
	if autostartEnabled() {
		as |= MF_CHECKED
	}
	add(m, as, cmdAutostart, T("Запускать вместе с Windows", "Start with Windows"))
	add(m, MF_SEPARATOR, 0, "")
	add(m, MF_STRING, cmdExit, T("Выход", "Exit"))

	var pt POINT
	pGetCursorPos.Call(ptr(&pt))
	pSetForegroundWindow.Call(hwndMain)
	cmd, _, _ := pTrackPopupMenu.Call(m, TPM_RIGHTBUTTON|TPM_RETURNCMD|TPM_NONOTIFY,
		uintptr(pt.X), uintptr(pt.Y), 0, hwndMain, 0)
	pPostMessageW.Call(hwndMain, WM_NULL, 0, 0)
	c := int(cmd)
	switch {
	case c == cmdOpenUI:
		openUI()
	case c == cmdAutostart:
		setAutostart(!autostartEnabled())
	case c == cmdApplyAll:
		id := snap.Active
		if id == "" {
			id = snap.Base
		}
		go eng.Apply(id, true, "из меню", true, false)
	case c == cmdExit:
		pDestroyWindow.Call(hwndMain)
	case c >= cmdProfile0 && c < cmdProfile0+len(ids):
		id := ids[c-cmdProfile0]
		store.mu.Lock()
		store.data.Settings.BaseProfile = id
		_ = store.saveData()
		store.mu.Unlock()
		api.notify()
		go eng.Apply(id, false, "из меню", true, false)
	}
}

// ---------------------------------------------------------------------------
// UI process

var (
	uiMu   sync.Mutex
	uiProc *exec.Cmd
)

func openUI() {
	// already open → bring to front
	if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16("webview"))), uintptr(unsafe.Pointer(u16(uiTitle)))); h != 0 {
		if r, _, _ := pIsIconic.Call(h); r != 0 {
			pShowWindow.Call(h, SW_RESTORE)
		}
		pSetForegroundWindow.Call(h)
		return
	}
	uiMu.Lock()
	running := uiProc != nil
	uiMu.Unlock()
	if running {
		return
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "--ui", api.url)
	if err := cmd.Start(); err != nil {
		shellOpen(api.url)
		return
	}
	pAllowSetForeground.Call(uintptr(cmd.Process.Pid))
	uiMu.Lock()
	uiProc = cmd
	uiMu.Unlock()
	go func() {
		start := time.Now()
		err := cmd.Wait()
		uiMu.Lock()
		uiProc = nil
		uiMu.Unlock()
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 && time.Since(start) < 60*time.Second {
			// WebView2 runtime not available → default browser
			uiCall(func() { shellOpen(api.url) })
		}
	}()
}

func shellOpen(path string) {
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(path))), 0, 0, 1)
}

// ---------------------------------------------------------------------------

func mainWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case msgTray:
		switch lp & 0xFFFF {
		case WM_LBUTTONUP:
			openUI()
		case WM_RBUTTONUP, WM_CONTEXTMENU:
			showMenu()
		}
		return 0
	case msgCall:
		callMu.Lock()
		q := calls
		calls = nil
		callMu.Unlock()
		for _, f := range q {
			f()
		}
		return 0
	case msgOpenUI:
		openUI()
		return 0
	case WM_DEVICECHANGE:
		pSetTimer.Call(hwnd, timerSoon, 1000, 0)
		return 0
	case WM_TIMER:
		switch wp {
		case timerSoon:
			pKillTimer.Call(hwnd, timerSoon)
			rescan()
		case timerRescan:
			rescan()
		}
		return 0
	case WM_DESTROY:
		pShellNotifyIconW.Call(NIM_DELETE, ptr(&nid))
		pPostQuitMessage.Call(0)
		return 0
	}
	if msgTaskbar != 0 && msg == msgTaskbar {
		addTray()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func fatal(text string) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16("SM809 Control"))), 0x10)
	os.Exit(1)
}

func setDPIAware() {
	if pSetProcessDpiAwarenessCtx.Find() == nil {
		pSetProcessDpiAwarenessCtx.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2
	} else {
		pSetProcessDPIAware.Call()
	}
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--ui" {
		runUI(os.Args[2])
		return
	}
	runtime.LockOSThread()

	_, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16(`Local\SM809ControlMutex`))))
	if errno, ok := err.(syscall.Errno); ok && errno == 183 {
		// second launch → ask the running instance to show its window
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16(mainClass))), 0); h != 0 {
			pAllowSetForeground.Call(^uintptr(0)) // ASFW_ANY
			pPostMessageW.Call(h, msgOpenUI, 0, 0)
		}
		return
	}
	setDPIAware()

	dir := appDataDir()
	logFile = filepath.Join(dir, "sm809control.log")
	setLang(systemLanguage()) // used for defaults created on the very first run
	store = loadStore(dir)
	if l := store.data.Settings.Language; l == "ru" || l == "en" {
		setLang(l)
	} else {
		store.data.Settings.Language = currentLang()
		_ = store.saveData()
	}
	logEnabled = store.data.Settings.Debug
	logf("запуск %s, данные: %s", appVersion, dir)

	hinst, _, _ = pGetModuleHandleW.Call(0)
	wc := WNDCLASSEXW{LpfnWndProc: syscall.NewCallback(mainWndProc), HInstance: hinst, LpszClassName: u16(mainClass)}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, _ := pRegisterClassExW.Call(ptr(&wc)); r == 0 {
		fatal(T("Не удалось зарегистрировать класс окна", "Cannot register the window class"))
	}
	hwndMain, _, _ = pCreateWindowExW.Call(WS_EX_TOOLWINDOW, uintptr(unsafe.Pointer(u16(mainClass))),
		uintptr(unsafe.Pointer(u16("SM809 Control"))), WS_POPUP, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwndMain == 0 {
		fatal(T("Не удалось создать окно", "Cannot create the window"))
	}
	msgTaskbar, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))

	eng = newEngine(store)
	eng.bat = loadBatHistory(dir)
	api, err = newAPI(eng, store, uiCall)
	if err != nil {
		fatal(T("Не удалось запустить локальный сервер: ", "Cannot start the local server: ") + err.Error())
	}
	var trayPending int32
	var trayMu sync.Mutex
	eng.onChange = func() {
		api.notify()
		trayMu.Lock()
		if trayPending == 0 {
			trayPending = 1
			trayMu.Unlock()
			uiCall(func() {
				trayMu.Lock()
				trayPending = 0
				trayMu.Unlock()
				updateTray()
			})
			return
		}
		trayMu.Unlock()
	}
	eng.onOSD = func(c osdContent) { uiCall(func() { osdShow(c) }) }
	eng.onNotify = func(t, s string, warn bool) { uiCall(func() { notify(t, s, warn) }) }
	soft = newSoftRuntime(store, eng, eng.onOSD)
	soft.notify = api.notify
	api.soft = soft
	soft.run()

	refreshAutostart()
	osdCreate(hinst)
	addTray()
	rescan()
	pSetTimer.Call(hwndMain, timerRescan, 5000, 0)
	go watchProcesses(eng, store)

	if !startedByAutorun() {
		openUI() // started by the user (autostart passes --tray)
	}

	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(ptr(&m), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(ptr(&m))
		pDispatchMessageW.Call(ptr(&m))
	}
	uiMu.Lock()
	if uiProc != nil && uiProc.Process != nil {
		_ = uiProc.Process.Kill()
	}
	uiMu.Unlock()
	soft.stop()
	store.mu.Lock()
	restore := softMode(store.data.Settings) && store.data.Settings.RestoreOnExit && store.state.Carrier != [softSlots]bool{}
	store.mu.Unlock()
	if restore {
		// one flash write: the buttons work without the app until the next start
		if err := eng.ApplyPlain(eng.OnMouse(), 3*time.Second); err != nil {
			logf("выход: обычная раскладка не записана: %v", err)
		}
	}
	if curIcon != 0 {
		pDestroyIcon.Call(curIcon)
	}
}

// startedByAutorun: the Run key starts us with --tray.
func startedByAutorun() bool {
	for _, a := range os.Args[1:] {
		if a == "--tray" {
			return true
		}
	}
	return false
}

