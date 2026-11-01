package main

// Button modes (2.0).
//
// Hardware mode: every button does what the mouse's flash says (as in 1.x).
//
// Software mode: buttons 1..11 are either hardware buttons (their firmware
// action is written to the mouse, as in hardware mode) or software buttons.
// A software button is written once as a key no keyboard has (F13..F23,
// "carrier"); a low-level keyboard hook catches it and performs the action of
// the profile that belongs to the program in front (rules "program →
// profile"), so switching between programs is instant and never writes the
// flash. Which buttons are software is one setting for all profiles, so the
// carrier layout on the mouse never depends on the profile. Buttons 12..16
// (left, right, middle, DPI+/−) are always hardware buttons. Without the app,
// software buttons do nothing.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	softSlots  = 16 // every button (indices 0..15) …
	lockedSlot = 11 // … except button 12, the main left click: always a hardware button

	ModeHardware = "hardware"
	ModeSoftware = "software"
)

// carrierKey is what a software button sends: a key no ordinary keyboard
// has. F13..F23 for buttons 1..11, F24 for button 13; buttons 14..16 use
// rare browser keys of the HID consumer page (Windows turns them into
// VK_BROWSER_SEARCH / FAVORITES / STOP), because the F keys run out.
type carrierKey struct {
	Code  [3]int // firmware action
	VK    uint32 // what the keyboard hook sees
	Label string
}

var carriers = func() [softSlots]carrierKey {
	var c [softSlots]carrierKey
	for i := 0; i < 11; i++ {
		c[i] = carrierKey{[3]int{112, 0, 104 + i}, uint32(0x7C + i), fmt.Sprintf("F%d", 13+i)}
	}
	c[12] = carrierKey{[3]int{112, 0, 115}, 0x87, "F24"}
	c[13] = carrierKey{[3]int{128, 0x21, 0x02}, 0xAA, "Browser Search"}    // AC Search 0x221
	c[14] = carrierKey{[3]int{128, 0x2A, 0x02}, 0xAB, "Browser Favorites"} // AC Bookmarks 0x22A
	c[15] = carrierKey{[3]int{128, 0x26, 0x02}, 0xA9, "Browser Stop"}      // AC Stop 0x226
	return c
}()

func softSlotName(i int) string { return "b" + strconv.Itoa(i+1) }

// softSlotIndex: "b1".."b11" → 0..10, otherwise -1.
func softSlotIndex(s string) int {
	if !strings.HasPrefix(s, "b") {
		return -1
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 1 || n > softSlots || s[1:] != strconv.Itoa(n) {
		return -1
	}
	return n - 1
}

func carrierVK(i int) uint32 { return carriers[i].VK }

// injectedMark goes into dwExtraInfo of every event the app injects.
const injectedMark = 0x534D3839 // "SM89"

// What the keyboard hook does with an event.
const (
	hookIgnore        = iota // pass through untouched
	hookKeyboard             // a real key press: carrier keys, capture, "until any key"
	hookSystemCarrier        // a carrier key Windows injected on the mouse's behalf
)

// hookEventKind sorts keyboard hook events. Real key presses are the
// F-key carriers. The browser-key carriers (HID consumer page) are turned
// into keystrokes by Windows itself, so they arrive *injected* (confirmed with
// keyprobe: scan 00 E0, no device); they are accepted as long as they are
// not ours (our own SendInput carries injectedMark).
func hookEventKind(vk uint32, injected bool, extra uintptr) int {
	if !injected {
		return hookKeyboard
	}
	if extra == injectedMark {
		return hookIgnore
	}
	if i := carrierSlotOfVK(vk); i >= 0 && carriers[i].Code[0] == 128 {
		return hookSystemCarrier
	}
	return hookIgnore
}

// carrierSlotOfVK: the button whose carrier key is vk, otherwise -1.
func carrierSlotOfVK(vk uint32) int {
	for i, c := range carriers {
		if i != lockedSlot && c.VK == vk {
			return i
		}
	}
	return -1
}

// Action kinds of a software button.
const (
	SoftNone    = ""        // not set: the button does nothing
	SoftKeys    = "keys"    // a key or a combination, held while the button is held
	SoftMouse   = "mouse"   // a mouse button, held while the button is held
	SoftDisable = "disable" // nothing (set on purpose)
	SoftMacro   = "macro"   // a macro from the Macros tab, played by the app
	SoftText    = "text"    // type a text (optionally a protected secret), then a key
	SoftRun     = "run"     // start a program, open a document, folder or URL

	softLegacyDefault = "default" // beta: "as in the mouse profile"
)

// Ways of typing a text.
const (
	TextUnicode = "unicode" // characters as Unicode input: independent of the keyboard layout
	TextKeys    = "keys"    // real key presses in a given layout (for apps that ignore Unicode input)
)

const (
	maxSoftText      = 2000 // characters
	maxSoftCharDelay = 500  // ms
)

var softMouseButtons = []string{"left", "right", "middle", "back", "forward"}

// SoftBinding is the action of a software button.
type SoftBinding struct {
	Kind    string `json:"kind"`
	Keys    string `json:"keys,omitempty"`    // "Ctrl+Win+Right"
	Button  string `json:"button,omitempty"`  // left, right, middle, back, forward
	MacroID string `json:"macroId,omitempty"` // SoftMacro

	// SoftText. A secret is kept only encrypted for the current Windows user
	// (Enc, DPAPI) and never sent back as plain text; Text then carries a new
	// value from the UI only.
	Text      string `json:"text,omitempty"`
	Enc       string `json:"enc,omitempty"`
	Secret    bool   `json:"secret,omitempty"`
	Method    string `json:"method,omitempty"`    // TextUnicode or TextKeys
	Layout    string `json:"layout,omitempty"`    // TextKeys: keyboard layout (HKL, hex); "" = the window's current one
	After     string `json:"after,omitempty"`     // key combination pressed after the text, e.g. "Enter"
	CharDelay int    `json:"charDelay,omitempty"` // ms between characters

	// SoftRun
	Path string `json:"path,omitempty"`
	Args string `json:"args,omitempty"`

	Force bool `json:"force,omitempty"` // beta only (global binding over profiles), dropped on migration
}

func softMode(s Settings) bool { return s.ButtonMode == ModeSoftware }

// ---------------------------------------------------------------------------
// Firmware action ↔ software action

// SoftOut is a concrete action the app performs.
type SoftOut struct {
	Kind   string // SoftKeys, SoftMouse, SoftDisable, SoftMacro, SoftText or SoftRun
	Combo  Combo
	Button string
	Macro  *Macro      // SoftMacro (a copy)
	B      SoftBinding // SoftText, SoftRun: the binding itself
}

var fwMouseButton = map[int]string{2: "right", 4: "middle", 8: "back", 16: "forward"}

// fwOutput translates a firmware key/mouse/media/disable action into a
// software output. ok = false for everything else and for the left click.
func fwOutput(b ButtonAssign) (SoftOut, bool) {
	if b.Kind == "macro" {
		return SoftOut{}, false
	}
	c := b.Code
	switch c[0] {
	case 16: // mouse button; the left click stays on the mouse
		if n, ok := fwMouseButton[c[1]]; ok && c[2] == 0 {
			return SoftOut{Kind: SoftMouse, Button: n}, true
		}
	case 96:
		if c[1] == 0 && c[2] == 0 {
			return SoftOut{Kind: SoftDisable}, true
		}
	case 112: // key / combination
		parts := fwModNames(c[1])
		if c[2] != 0 {
			n, ok := hidUsageName[c[2]]
			if !ok {
				return SoftOut{}, false
			}
			parts = append(parts, n)
		}
		if combo, err := parseCombo(strings.Join(parts, "+")); err == nil {
			return SoftOut{Kind: SoftKeys, Combo: combo}, true
		}
	case 128: // consumer usage, 16-bit little endian
		if n, ok := consumerName[c[1]|c[2]<<8]; ok {
			if combo, err := parseCombo(n); err == nil {
				return SoftOut{Kind: SoftKeys, Combo: combo}, true
			}
		}
	}
	return SoftOut{}, false
}

// softFromFirmware: the software action equivalent to a firmware action
// (Kind "" if there is none: rapid fire, DPI, DPI lock, lighting, left click).
func softFromFirmware(b ButtonAssign) SoftBinding {
	if b.Kind == "macro" && b.MacroID != "" {
		return SoftBinding{Kind: SoftMacro, MacroID: b.MacroID}
	}
	o, ok := fwOutput(b)
	if !ok {
		return SoftBinding{}
	}
	switch o.Kind {
	case SoftKeys:
		return SoftBinding{Kind: SoftKeys, Keys: o.Combo.String()}
	case SoftMouse:
		return SoftBinding{Kind: SoftMouse, Button: o.Button}
	}
	return SoftBinding{Kind: SoftDisable}
}

// defaultHW: on first switch to software mode, buttons whose action exists
// only in the firmware stay hardware buttons; the rest become software
// buttons doing the same thing.
func defaultHW(p *Profile) [softSlots]bool {
	var hw [softSlots]bool
	for i := 0; i < softSlots; i++ {
		// buttons 12..16 (left, right, middle, DPI) start as hardware buttons;
		// each can be made a software button later (except the left click)
		hw[i] = i >= lockedSlot || softFromFirmware(p.Buttons[i]).Kind == ""
	}
	return hw
}

// fillSoft gives software buttons without an action the equivalent of their
// firmware action, so a button keeps doing what it did.
func fillSoft(p *Profile, hw [softSlots]bool) {
	for i := 0; i < softSlots; i++ {
		if !hw[i] && p.Soft[i].Kind == SoftNone {
			p.Soft[i] = softFromFirmware(p.Buttons[i])
		}
	}
}

// carriedMask: buttons that send F13..F23 in the given settings.
func carriedMask(s Settings) [softSlots]bool {
	var m [softSlots]bool
	if softMode(s) {
		for i := range m {
			m[i] = i != lockedSlot && !s.HWButtons[i]
		}
	}
	return m
}

// carrierProfile returns a copy of p whose carried buttons send F13..F23.
func carrierProfile(p *Profile, m [softSlots]bool) *Profile {
	c := *p
	for i := 0; i < softSlots; i++ {
		if m[i] && i != lockedSlot {
			kind := "key"
			if carriers[i].Code[0] == 128 {
				kind = "media"
			}
			c.Buttons[i] = ButtonAssign{Kind: kind, Code: carriers[i].Code, Label: carriers[i].Label}
		}
	}
	return &c
}

// deviceProfile is what is written to the mouse for p.
func deviceProfile(p *Profile, s Settings) *Profile {
	m := carriedMask(s)
	if m == ([softSlots]bool{}) {
		return p
	}
	return carrierProfile(p, m)
}

// deviceHasLeft: the layout written to the mouse keeps a left click.
func deviceHasLeft(p *Profile, s Settings) bool {
	for _, b := range deviceProfile(p, s).Buttons {
		if b.Kind != "macro" && b.Code == [3]int{16, 1, 0} {
			return true
		}
	}
	return false
}

// carrierInMemory: which buttons of a profile read back from the mouse send
// their carrier key.
func carrierInMemory(p *Profile) [softSlots]bool {
	var m [softSlots]bool
	for i := 0; i < softSlots; i++ {
		m[i] = i != lockedSlot && p.Buttons[i].Kind != "macro" && p.Buttons[i].Code == carriers[i].Code
	}
	return m
}

// ---------------------------------------------------------------------------
// Which profile drives the software buttons

// ruleProfileFor: the profile of the first enabled rule for exe ("" = none).
func ruleProfileFor(d *Data, exe string) (string, *Rule) {
	exe = normalizeExe(exe)
	if exe == "" {
		return "", nil
	}
	for i := range d.Rules {
		r := &d.Rules[i]
		if r.Enabled && r.Exe == exe {
			return r.ProfileID, r
		}
	}
	return "", nil
}

// softOutput is what software button i of profile p does. ok = false: nothing.
func softOutput(p *Profile, i int, macroOf func(string) *Macro) (SoftOut, bool) {
	if p == nil || i < 0 || i >= softSlots {
		return SoftOut{}, false
	}
	b := p.Soft[i]
	switch b.Kind {
	case SoftKeys:
		c, err := parseCombo(b.Keys)
		if err != nil {
			return SoftOut{}, false
		}
		return SoftOut{Kind: SoftKeys, Combo: c}, true
	case SoftMouse:
		return SoftOut{Kind: SoftMouse, Button: b.Button}, true
	case SoftMacro:
		if macroOf == nil {
			return SoftOut{}, false
		}
		m := macroOf(b.MacroID)
		if m == nil || len(m.Events) == 0 {
			return SoftOut{}, false
		}
		c := *m
		c.Events = append([]MacroEvent(nil), m.Events...)
		return SoftOut{Kind: SoftMacro, Macro: &c}, true
	case SoftText, SoftRun:
		return SoftOut{Kind: b.Kind, B: b}, true
	case SoftDisable:
		return SoftOut{Kind: SoftDisable}, true
	}
	return SoftOut{}, false
}

// ---------------------------------------------------------------------------
// Runtime status and the platform part

// SoftStatus is shown in the UI.
type SoftStatus struct {
	Enabled   bool            `json:"enabled"`   // software mode
	Mode      string          `json:"mode"`      // "", hardware, software
	Carrier   [softSlots]bool `json:"carrier"`   // buttons that send F13..F23 now (last write / read)
	Carriable [softSlots]bool `json:"carriable"` // buttons that should (software buttons)
	OnMouse   string          `json:"onMouse"`   // id of the profile on the mouse
	Hook      bool            `json:"hook"`      // the keyboard hook is installed
	Exe       string          `json:"exe"`       // foreground program
	Profile   string          `json:"profile"`   // id of the profile driving the software buttons
	Held      []string        `json:"held"`      // buttons currently held (live feedback)
	Last      string          `json:"last"`      // last handled button
	Error     string          `json:"error"`     // last action error (shown for a minute)
}

// softHost is the platform part (keyboard hook, SendInput, foreground).
type softHost interface {
	status() SoftStatus
	capture(timeout time.Duration) (string, error)
	changed()                  // settings, profiles or carrier changed
	layouts() []keyboardLayout // installed keyboard layouts
}

type keyboardLayout struct {
	ID   string `json:"id"`   // HKL, hex
	Name string `json:"name"` // language name
}

// ---------------------------------------------------------------------------
// Secrets and macro references

// protectSecret / unprotectSecret encrypt a text for the current Windows user
// (DPAPI). Set on Windows; nil elsewhere.
var (
	protectSecret   func(string) (string, error)
	unprotectSecret func(string) (string, error)
)

// sealSecrets moves new secret texts of p into Enc.
func sealSecrets(p *Profile) error {
	for i := range p.Soft {
		b := &p.Soft[i]
		if b.Kind != SoftText || !b.Secret || b.Text == "" {
			continue
		}
		if protectSecret == nil {
			return errors.New(T("шифрование недоступно", "encryption is not available"))
		}
		enc, err := protectSecret(b.Text)
		if err != nil {
			return fmt.Errorf("%s: %v", softSlotLabel(i), err)
		}
		b.Enc, b.Text = enc, ""
	}
	return nil
}

// softMacroUse: where a macro is used by a software button ("" = nowhere).
func softMacroUse(d *Data, id string) string {
	for _, p := range d.Profiles {
		for i, b := range p.Soft {
			if b.Kind == SoftMacro && b.MacroID == id {
				return p.Name + ", " + softSlotLabel(i)
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Validation

func softSlotLabel(i int) string { return T("кнопка ", "button ") + strconv.Itoa(i+1) }

func validSoftMouseButton(s string) bool {
	for _, b := range softMouseButtons {
		if b == s {
			return true
		}
	}
	return false
}

// validateSoftBinding normalises b in place.
func validateSoftBinding(i int, b *SoftBinding) error {
	at := softSlotLabel(i)
	b.Force = false
	switch b.Kind {
	case SoftNone, SoftDisable:
	case SoftKeys:
		c, err := parseCombo(b.Keys)
		if err != nil {
			return fmt.Errorf("%s: %v", at, err)
		}
		b.Keys = c.String()
	case SoftMouse:
		if !validSoftMouseButton(b.Button) {
			return fmt.Errorf(T("%s: неизвестная кнопка мыши %q", "%s: unknown mouse button %q"), at, b.Button)
		}
	case SoftMacro:
		if b.MacroID == "" {
			return fmt.Errorf(T("%s: макрос не выбран", "%s: no macro selected"), at)
		}
	case SoftText:
		if b.Text == "" && b.Enc == "" {
			return fmt.Errorf(T("%s: текст не задан", "%s: no text"), at)
		}
		if len([]rune(b.Text)) > maxSoftText {
			return fmt.Errorf(T("%s: текст длиннее %d символов", "%s: the text is longer than %d characters"), at, maxSoftText)
		}
		switch b.Method {
		case "":
			b.Method = TextUnicode
		case TextUnicode, TextKeys:
		default:
			return fmt.Errorf(T("%s: неизвестный способ ввода %q", "%s: unknown input method %q"), at, b.Method)
		}
		if b.Method != TextKeys {
			b.Layout = ""
		} else if b.Layout != "" {
			if _, err := strconv.ParseUint(b.Layout, 16, 64); err != nil || len(b.Layout) > 16 {
				return fmt.Errorf(T("%s: неверная раскладка %q", "%s: invalid layout %q"), at, b.Layout)
			}
		}
		if strings.TrimSpace(b.After) != "" {
			c, err := parseCombo(b.After)
			if err != nil {
				return fmt.Errorf(T("%s: клавиша после текста: %v", "%s: key after the text: %v"), at, err)
			}
			b.After = c.String()
		} else {
			b.After = ""
		}
		if b.CharDelay < 0 {
			b.CharDelay = 0
		}
		if b.CharDelay > maxSoftCharDelay {
			b.CharDelay = maxSoftCharDelay
		}
		if !b.Secret && b.Text == "" { // a secret was turned off without a new text
			return fmt.Errorf(T("%s: введите текст заново — сохранённый секрет не показывается", "%s: enter the text again — a stored secret is never shown"), at)
		}
		if !b.Secret {
			b.Enc = ""
		}
	case SoftRun:
		b.Path = strings.Trim(strings.TrimSpace(b.Path), `"`)
		b.Args = strings.TrimSpace(b.Args)
		if b.Path == "" {
			return fmt.Errorf(T("%s: не указано, что запускать", "%s: nothing to start"), at)
		}
	default:
		return fmt.Errorf(T("%s: неизвестное действие %q", "%s: unknown action %q"), at, b.Kind)
	}
	if b.Kind != SoftKeys {
		b.Keys = ""
	}
	if b.Kind != SoftMouse {
		b.Button = ""
	}
	if b.Kind != SoftMacro {
		b.MacroID = ""
	}
	if b.Kind != SoftText {
		b.Text, b.Enc, b.Secret, b.Method, b.Layout, b.After, b.CharDelay = "", "", false, "", "", "", 0
	}
	if b.Kind != SoftRun {
		b.Path, b.Args = "", ""
	}
	return nil
}

func validateProfileSoft(p *Profile) error {
	for i := range p.Soft {
		if err := validateSoftBinding(i, &p.Soft[i]); err != nil {
			return err
		}
	}
	return nil
}

func normalizeExe(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, `"`)
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// ---------------------------------------------------------------------------
// Migration of the 1.5 betas (separate "soft buttons" with their own
// global bindings and program profiles) into 2.0 profiles and rules.

type SoftProfile struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Exes     []string               `json:"exes"`
	Enabled  bool                   `json:"enabled"`
	Bindings map[string]SoftBinding `json:"bindings"`
	OSD      bool                   `json:"osd"`
}

type SoftData struct {
	Global        map[string]SoftBinding `json:"global"`
	Profiles      []*SoftProfile         `json:"profiles"`
	KeyRepeat     bool                   `json:"keyRepeat"`
	RestoreOnExit bool                   `json:"restoreOnExit"`
}

// migrateBeta converts d.Soft. carrier is what the mouse holds now (so the
// set of software buttons does not change). base is the profile copied for
// beta program profiles.
func migrateBeta(d *Data, carrier [softSlots]bool, base *Profile) {
	sd := d.Soft
	d.Soft = nil
	if sd == nil {
		return
	}
	use := func(b SoftBinding) (SoftBinding, bool) {
		if b.Kind == SoftNone || b.Kind == softLegacyDefault {
			return SoftBinding{}, false
		}
		b.Force = false
		if validateSoftBinding(0, &b) != nil {
			return SoftBinding{}, false
		}
		return b, true
	}
	for _, p := range d.Profiles {
		for i := 0; i < softSlots; i++ {
			if b, ok := use(sd.Global[softSlotName(i)]); ok {
				p.Soft[i] = b
			} else {
				p.Soft[i] = softFromFirmware(p.Buttons[i])
			}
		}
	}
	if base != nil {
		for _, sp := range sd.Profiles {
			if sp == nil {
				continue
			}
			np := *base
			np.ID = newID("p")
			np.Name = sp.Name
			for slot, b := range sp.Bindings {
				if i := softSlotIndex(slot); i >= 0 {
					if v, ok := use(b); ok {
						np.Soft[i] = v
					} else if b.Kind == softLegacyDefault {
						np.Soft[i] = softFromFirmware(np.Buttons[i])
					}
				}
			}
			d.Profiles = append(d.Profiles, &np)
			for _, e := range sp.Exes {
				e = normalizeExe(e)
				taken := false
				for _, r := range d.Rules {
					taken = taken || r.Exe == e
				}
				if e != "" && !taken {
					d.Rules = append(d.Rules, Rule{Exe: e, ProfileID: np.ID, OSD: sp.OSD, Enabled: sp.Enabled})
				}
			}
		}
	}
	d.Settings.KeyRepeat, d.Settings.RestoreOnExit = sd.KeyRepeat, sd.RestoreOnExit
	if d.Settings.SoftBindings {
		d.Settings.ButtonMode = ModeSoftware
		if carrier == ([softSlots]bool{}) && base != nil {
			d.Settings.HWButtons = defaultHW(base)
		} else {
			for i := range carrier {
				d.Settings.HWButtons[i] = i >= lockedSlot || !carrier[i] // the betas carried buttons 1..11 only
			}
		}
	}
	d.Settings.SoftBindings = false
}
