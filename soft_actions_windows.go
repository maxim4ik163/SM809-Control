//go:build windows

package main

// Actions of soft buttons beyond plain keys: macros played by the app, typing
// a text (optionally a DPAPI-protected secret), starting a program. Also the
// keyboard layout list and the file picker of the settings window.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	crypt32  = syscall.NewLazyDLL("crypt32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")

	pCryptProtectData          = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData        = crypt32.NewProc("CryptUnprotectData")
	pLocalFree                 = kernel32.NewProc("LocalFree")
	pExpandEnvironmentStringsW = kernel32.NewProc("ExpandEnvironmentStringsW")
	pGetLocaleInfoW            = kernel32.NewProc("GetLocaleInfoW")
	pGetKeyboardLayoutList     = user32.NewProc("GetKeyboardLayoutList")
	pGetKeyboardLayout         = user32.NewProc("GetKeyboardLayout")
	pVkKeyScanExW              = user32.NewProc("VkKeyScanExW")
	pMapVirtualKeyExW          = user32.NewProc("MapVirtualKeyExW")
	pGetKeyState               = user32.NewProc("GetKeyState")
	pGetOpenFileNameW          = comdlg32.NewProc("GetOpenFileNameW")
)

const (
	KEYEVENTF_UNICODE          = 0x0004
	MOUSEEVENTF_MOVE           = 0x0001
	WM_INPUTLANGCHANGEREQUEST  = 0x0050
	CRYPTPROTECT_UI_FORBIDDEN  = 0x1
	LOCALE_SLOCALIZEDDISPLAYNM = 0x2
	secretPrefix               = "dpapi:"
)

func init() {
	protectSecret = dpapiProtect
	unprotectSecret = dpapiUnprotect
}

// ---------------------------------------------------------------------------
// DPAPI: secrets can be decrypted only by the same Windows user on this PC.

type dataBlob struct {
	Size uint32
	Data *byte
}

func dpapiProtect(s string) (string, error) {
	b := []byte(s)
	in := dataBlob{Size: uint32(len(b))}
	if len(b) > 0 {
		in.Data = &b[0]
	}
	var out dataBlob
	r, _, err := pCryptProtectData.Call(ptr(&in), uintptr(unsafe.Pointer(u16("SM809 Control"))), 0, 0, 0, CRYPTPROTECT_UI_FORBIDDEN, ptr(&out))
	if r == 0 {
		return "", err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.Data)))
	enc := unsafe.Slice(out.Data, out.Size)
	return secretPrefix + base64.StdEncoding.EncodeToString(enc), nil
}

func dpapiUnprotect(s string) (string, error) {
	if !strings.HasPrefix(s, secretPrefix) {
		return "", errors.New("not a DPAPI secret")
	}
	b, err := base64.StdEncoding.DecodeString(s[len(secretPrefix):])
	if err != nil || len(b) == 0 {
		return "", errors.New("damaged secret")
	}
	in := dataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out dataBlob
	r, _, e := pCryptUnprotectData.Call(ptr(&in), 0, 0, 0, 0, CRYPTPROTECT_UI_FORBIDDEN, ptr(&out))
	if r == 0 {
		return "", e
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}

// ---------------------------------------------------------------------------
// Keyboard layouts

func installedLayouts() []uintptr {
	n, _, _ := pGetKeyboardLayoutList.Call(0, 0)
	if n == 0 || n > 64 {
		return nil
	}
	buf := make([]uintptr, n)
	n, _, _ = pGetKeyboardLayoutList.Call(n, uintptr(unsafe.Pointer(&buf[0])))
	return buf[:n]
}

func hklID(h uintptr) string { return strings.ToUpper(strconv.FormatUint(uint64(h), 16)) }

func (r *softRuntime) layouts() []keyboardLayout {
	out := []keyboardLayout{}
	for _, h := range installedLayouts() {
		lang := h & 0xFFFF
		buf := make([]uint16, 128)
		name := ""
		if n, _, _ := pGetLocaleInfoW.Call(lang, LOCALE_SLOCALIZEDDISPLAYNM, uintptr(unsafe.Pointer(&buf[0])), 128); n > 0 {
			name = syscall.UTF16ToString(buf)
		}
		if name == "" {
			name = fmt.Sprintf("%04X", lang)
		}
		if (h>>16)&0xFFFF != lang { // a variant (e.g. Dvorak, typewriter)
			name += fmt.Sprintf(" (%X)", (h>>16)&0xFFFF)
		}
		out = append(out, keyboardLayout{ID: hklID(h), Name: name})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

func findLayout(id string) uintptr {
	for _, h := range installedLayouts() {
		if strings.EqualFold(hklID(h), id) {
			return h
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Typing a text

var typingMu sync.Mutex // one text at a time

func unicodeChar(u uint16) []INPUT {
	return []INPUT{
		keyInput(KEYBDINPUT{Scan: u, Flags: KEYEVENTF_UNICODE, ExtraInfo: injectedMark}),
		keyInput(KEYBDINPUT{Scan: u, Flags: KEYEVENTF_UNICODE | KEYEVENTF_KEYUP, ExtraInfo: injectedMark}),
	}
}

func vkTap(vk uint16, ext bool, hkl uintptr) []INPUT {
	scan, _, _ := pMapVirtualKeyExW.Call(uintptr(vk), 0, hkl)
	var fl uint32
	if ext {
		fl = KEYEVENTF_EXTENDEDKEY
	}
	return []INPUT{
		keyInput(KEYBDINPUT{Vk: vk, Scan: uint16(scan), Flags: fl, ExtraInfo: injectedMark}),
		keyInput(KEYBDINPUT{Vk: vk, Scan: uint16(scan), Flags: fl | KEYEVENTF_KEYUP, ExtraInfo: injectedMark}),
	}
}

// keysForChar: the key presses that produce ch in layout hkl, or nil.
func keysForChar(ch rune, hkl uintptr) []INPUT {
	if ch > 0xFFFF {
		return nil
	}
	res, _, _ := pVkKeyScanExW.Call(uintptr(ch), hkl)
	v := int16(res)
	if v == -1 {
		return nil
	}
	vk, shift := uint16(v&0xFF), uint16(v>>8)&0xFF
	if shift&0x38 != 0 { // Hankaku and other special shift states
		return nil
	}
	mods := []uint16{}
	if shift&2 != 0 {
		mods = append(mods, 0xA2) // Ctrl (with Alt: AltGr)
	}
	if shift&4 != 0 {
		mods = append(mods, 0xA4) // Alt
	}
	if shift&1 != 0 {
		mods = append(mods, 0xA0) // Shift
	}
	var in []INPUT
	for _, m := range mods {
		sc, _, _ := pMapVirtualKeyExW.Call(uintptr(m), 0, hkl)
		in = append(in, keyInput(KEYBDINPUT{Vk: m, Scan: uint16(sc), ExtraInfo: injectedMark}))
	}
	in = append(in, vkTap(vk, false, hkl)...)
	for i := len(mods) - 1; i >= 0; i-- {
		sc, _, _ := pMapVirtualKeyExW.Call(uintptr(mods[i]), 0, hkl)
		in = append(in, keyInput(KEYBDINPUT{Vk: mods[i], Scan: uint16(sc), Flags: KEYEVENTF_KEYUP, ExtraInfo: injectedMark}))
	}
	return in
}

// typeText types b's text into the window in front, then presses b.After.
func (r *softRuntime) typeText(b SoftBinding) {
	text := b.Text
	if b.Secret {
		if unprotectSecret == nil {
			return
		}
		t, err := unprotectSecret(b.Enc)
		if err != nil {
			r.setError(T("Текст не расшифрован (другой пользователь Windows или ПК?) — задайте его заново", "Could not decrypt the text (another Windows user or PC?) — enter it again"))
			logf("секрет не расшифрован: %v", err)
			return
		}
		text = t
	}
	typingMu.Lock()
	defer typingMu.Unlock()
	wait := func() {
		if b.CharDelay > 0 {
			time.Sleep(time.Duration(b.CharDelay) * time.Millisecond)
		}
	}
	special := func(ch rune, hkl uintptr) []INPUT {
		switch ch {
		case '\n':
			return vkTap(0x0D, false, hkl)
		case '\t':
			return vkTap(0x09, false, hkl)
		case '\r':
			return []INPUT{}
		}
		return nil
	}

	if b.Method != TextKeys {
		for _, ch := range text {
			if in := special(ch, 0); in != nil {
				sendInputs(in)
			} else {
				for _, u := range utf16.Encode([]rune{ch}) {
					sendInputs(unicodeChar(u))
				}
			}
			wait()
		}
	} else {
		fg, _, _ := pGetForegroundWindow.Call()
		tid, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
		cur, _, _ := pGetKeyboardLayout.Call(tid)
		hkl, switched := cur, false
		if b.Layout != "" {
			if want := findLayout(b.Layout); want == 0 {
				r.setError(T("Раскладка для ввода текста не установлена в Windows — печатаю в текущей", "The layout for typing is not installed in Windows — typing in the current one"))
			} else if want != cur && fg != 0 {
				pPostMessageW.Call(fg, WM_INPUTLANGCHANGEREQUEST, 0, want)
				for i := 0; i < 30; i++ { // up to 300 ms for the window to switch
					time.Sleep(10 * time.Millisecond)
					if h, _, _ := pGetKeyboardLayout.Call(tid); h == want {
						break
					}
				}
				hkl, switched = want, true
			}
		}
		caps := false
		if s, _, _ := pGetKeyState.Call(0x14); s&1 != 0 { // Caps Lock would invert letters
			caps = true
			sendInputs(vkTap(0x14, false, hkl))
		}
		for _, ch := range text {
			in := special(ch, hkl)
			if in == nil {
				in = keysForChar(ch, hkl)
			}
			if in == nil { // not on this layout: Unicode input for this character
				for _, u := range utf16.Encode([]rune{ch}) {
					in = append(in, unicodeChar(u)...)
				}
			}
			sendInputs(in)
			wait()
		}
		if caps {
			sendInputs(vkTap(0x14, false, hkl))
		}
		if switched {
			r.pressAfter(b.After)
			// posted messages overtake keyboard input in the target's queue:
			// give it time to process the keys before switching back
			time.Sleep(250 * time.Millisecond)
			pPostMessageW.Call(fg, WM_INPUTLANGCHANGEREQUEST, 0, cur)
			return
		}
	}
	r.pressAfter(b.After)
}

func (r *softRuntime) pressAfter(after string) {
	if after == "" {
		return
	}
	c, err := parseCombo(after)
	if err != nil {
		return
	}
	time.Sleep(30 * time.Millisecond)
	pressCombo(c)
	releaseCombo(c)
}

// ---------------------------------------------------------------------------
// Starting a program

func expandEnv(s string) string {
	src := u16(s)
	buf := make([]uint16, 4096)
	n, _, _ := pExpandEnvironmentStringsW.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return s
	}
	return syscall.UTF16ToString(buf)
}

func (r *softRuntime) runProgram(b SoftBinding) {
	path := expandEnv(b.Path)
	dir := ""
	if filepath.IsAbs(path) {
		dir = filepath.Dir(path)
	}
	var args, wd uintptr
	if b.Args != "" {
		args = uintptr(unsafe.Pointer(u16(expandEnv(b.Args))))
	}
	if dir != "" {
		wd = uintptr(unsafe.Pointer(u16(dir)))
	}
	pAllowSetForeground.Call(^uintptr(0)) // ASFW_ANY: the started program may come to the front
	res, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(path))), args, wd, 1)
	if res <= 32 {
		r.setError(fmt.Sprintf(T("Не удалось запустить %s (код %d)", "Could not start %s (code %d)"), b.Path, res))
		logf("запуск %q: ShellExecute %d", path, res)
		return
	}
	logf("запущено: %s %s", path, b.Args)
}

// ---------------------------------------------------------------------------
// Macros played by the app
//
// Same macros as in the Macros tab and the same modes: 1 = repeat while the
// button is held, 2 = run N times, 3 = repeat until the button is pressed
// again. Unlike the firmware, the app also performs cursor moves.

type macroRun struct {
	slot int
	mode int
	stop chan struct{}
	once sync.Once
	done chan struct{}
}

func (m *macroRun) halt() { m.once.Do(func() { close(m.stop) }) }

func (m *macroRun) running() bool {
	select {
	case <-m.done:
		return false
	default:
		return true
	}
}

var macroMouse = map[int]string{240: "left", 241: "right", 242: "middle", 243: "back", 244: "forward"}

// macroKey: macro key code → key definition (HID usage or modifier usage).
func macroKey(code int) (keyDef, bool) {
	n, ok := hidUsageName[code]
	if !ok {
		return keyDef{}, false
	}
	k, ok := keyByName[strings.ToLower(n)]
	return k, ok
}

func playMacro(m *Macro, run *macroRun) {
	defer close(run.done)
	ev := append([]MacroEvent(nil), m.Events...)
	sort.SliceStable(ev, func(a, b int) bool { return ev[a].Index < ev[b].Index })
	keysDown := map[int]bool{}
	defer func() { // never leave a key or button pressed
		for code := range keysDown {
			macroEvent(MacroEvent{Type: "keyup", Code: code})
		}
	}()
	sleep := func(ms int) bool {
		if ms <= 0 {
			select {
			case <-run.stop:
				return false
			default:
				return true
			}
		}
		select {
		case <-run.stop:
			return false
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return true
		}
	}
	total := 0
	for _, e := range ev {
		total += e.Delay
	}
	n := 1
	if run.mode == 2 {
		n = m.RepeatCount
		if n < 1 {
			n = 1
		}
	}
	for it := 0; run.mode != 2 || it < n; it++ {
		for _, e := range ev {
			if !sleep(e.Delay) {
				return
			}
			macroEvent(e)
			switch e.Type {
			case "keydown":
				keysDown[e.Code] = true
			case "keyup":
				delete(keysDown, e.Code)
			}
		}
		if total < 10 && !sleep(10) { // no busy loop for a macro without delays
			return
		}
	}
}

func macroEvent(e MacroEvent) {
	switch e.Type {
	case "keydown", "keyup":
		up := e.Type == "keyup"
		if b, ok := macroMouse[e.Code]; ok {
			sendMouseButton(b, up)
			return
		}
		if k, ok := macroKey(e.Code); ok {
			if up {
				releaseCombo(Combo{Keys: []keyDef{k}})
			} else {
				pressCombo(Combo{Keys: []keyDef{k}})
			}
		}
	case "cursorMove":
		v := e.Value
		if v == 0 {
			v = 1
		}
		var dx, dy int32
		switch e.Direction {
		case "left":
			dx = int32(-v)
		case "right":
			dx = int32(v)
		case "up":
			dy = int32(-v)
		case "down":
			dy = int32(v)
		}
		sendInputs([]INPUT{mouseInput(MOUSEINPUT{Dx: dx, Dy: dy, Flags: MOUSEEVENTF_MOVE, ExtraInfo: injectedMark})})
	}
}

// ---------------------------------------------------------------------------
// File picker (settings window process)

type openFileName struct {
	StructSize    uint32
	Owner         uintptr
	Instance      uintptr
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	FnHook        uintptr
	TemplateName  *uint16
	Reserved      uintptr
	Reserved2     uint32
	FlagsEx       uint32
}

// pickFile shows the Open dialog on a dedicated thread; "" = cancelled.
func pickFile(owner uintptr, start string) string {
	res := make(chan string, 1)
	go func() {
		runtime.LockOSThread() // the thread ends with the goroutine
		buf := make([]uint16, 1024)
		filter := utf16.Encode([]rune(T("Программы и ярлыки\x00*.exe;*.lnk;*.bat;*.cmd;*.ps1\x00Все файлы\x00*.*\x00\x00",
			"Programs and shortcuts\x00*.exe;*.lnk;*.bat;*.cmd;*.ps1\x00All files\x00*.*\x00\x00")))
		ofn := openFileName{Owner: owner, Filter: &filter[0], File: &buf[0], MaxFile: uint32(len(buf)),
			// FILEMUSTEXIST | PATHMUSTEXIST | NOCHANGEDIR | EXPLORER | NODEREFERENCELINKS
			Flags: 0x1000 | 0x800 | 0x8 | 0x80000 | 0x100000}
		ofn.StructSize = uint32(unsafe.Sizeof(ofn))
		if start != "" {
			if d := filepath.Dir(expandEnv(start)); d != "." {
				ofn.InitialDir = u16(d)
			}
		}
		if r, _, _ := pGetOpenFileNameW.Call(ptr(&ofn)); r == 0 {
			res <- ""
			return
		}
		res <- syscall.UTF16ToString(buf)
	}()
	return <-res
}
