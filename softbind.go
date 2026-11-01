package main

// Software bindings ("soft mode").
//
// The mouse is written once with a carrier layout: every remappable button
// (1..11) sends a key that no ordinary keyboard has, F13..F23. A low-level
// keyboard hook catches those keys, swallows them and sends whatever the
// active binding says. Switching bindings between programs changes only the
// app's memory; the mouse's flash is not written again.
//
// A slot is carried only if what the mouse profile assigns to it can be
// reproduced by the app (keys, media keys, mouse buttons except the left
// click, "disabled"). Firmware-only actions (macros, rapid fire, DPI, DPI lock,
// lighting toggle) and any left click stay on the mouse unchanged, so the
// left click can never be lost and nothing the user set up silently changes.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	softSlots        = 11   // buttons 1..11 (indices 0..10)
	carrierUsageBase = 104  // HID usage of F13
	carrierVKBase    = 0x7C // VK_F13
)

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

func carrierVK(i int) uint32 { return uint32(carrierVKBase + i) }

// carrierSlotOfVK: VK_F13..VK_F23 → slot index, otherwise -1.
func carrierSlotOfVK(vk uint32) int {
	if vk >= carrierVKBase && vk < carrierVKBase+softSlots {
		return int(vk - carrierVKBase)
	}
	return -1
}

// Binding kinds.
const (
	SoftInherit = ""        // not set here: fall back (profile → global → default)
	SoftDefault = "default" // what the mouse profile assigns to the button
	SoftKeys    = "keys"    // a key or a combination, held while the button is held
	SoftMouse   = "mouse"   // a mouse button, held while the button is held
	SoftDisable = "disable" // nothing
)

var softMouseButtons = []string{"left", "right", "middle", "back", "forward"}

type SoftBinding struct {
	Kind   string `json:"kind"`
	Keys   string `json:"keys,omitempty"`   // "Ctrl+Win+Right"
	Button string `json:"button,omitempty"` // left, right, middle, back, forward
	// Force (global bindings only): wins over any program profile.
	Force bool `json:"force,omitempty"`
}

// SoftProfile is a set of bindings that is active while one of its programs
// has the foreground window.
type SoftProfile struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Exes     []string               `json:"exes"` // lower-case file names, e.g. "acad.exe"
	Enabled  bool                   `json:"enabled"`
	Bindings map[string]SoftBinding `json:"bindings"`
	OSD      bool                   `json:"osd"` // popup when this profile becomes active
}

type SoftData struct {
	Global   map[string]SoftBinding `json:"global"`
	Profiles []*SoftProfile         `json:"profiles"`
	// KeyRepeat: typematic repeat for a held single key (Windows does not
	// repeat injected keys by itself).
	KeyRepeat bool `json:"keyRepeat"`
	// RestoreOnExit writes the normal layout back when the app exits (one
	// flash write per exit, and another one at the next start).
	RestoreOnExit bool `json:"restoreOnExit"`
}

func defaultSoftData() *SoftData {
	return &SoftData{Global: map[string]SoftBinding{}, Profiles: []*SoftProfile{}, KeyRepeat: true}
}

// ---------------------------------------------------------------------------
// What a button does without the app → software output

// SoftOut is a concrete action the app performs.
type SoftOut struct {
	Kind   string // SoftKeys, SoftMouse or SoftDisable
	Combo  Combo
	Button string
}

var fwMouseButton = map[int]string{2: "right", 4: "middle", 8: "back", 16: "forward"}

// fwOutput translates a firmware button action into a software output.
// ok = false: the action can only be done by the mouse (or is the left click).
func fwOutput(b ButtonAssign) (SoftOut, bool) {
	if b.Kind == "macro" {
		return SoftOut{}, false
	}
	c := b.Code
	switch c[0] {
	case 16: // mouse button; the left click is never carried
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

// carriedMask: which of buttons 1..11 get a carrier key for this profile.
func carriedMask(p *Profile) [softSlots]bool {
	var m [softSlots]bool
	for i := 0; i < softSlots; i++ {
		_, m[i] = fwOutput(p.Buttons[i])
	}
	return m
}

// carrierProfile returns a copy of p whose carried buttons send F13..F23.
func carrierProfile(p *Profile) *Profile {
	c := *p
	m := carriedMask(p)
	for i := 0; i < softSlots; i++ {
		if m[i] {
			c.Buttons[i] = ButtonAssign{Kind: "key", Code: [3]int{112, 0, carrierUsageBase + i}, Label: fmt.Sprintf("F%d", 13+i)}
		}
	}
	return &c
}

// carrierInMemory: which buttons of a profile read back from the mouse send
// their carrier key (F13 on button 1 … F23 on button 11).
func carrierInMemory(p *Profile) [softSlots]bool {
	var m [softSlots]bool
	for i := 0; i < softSlots; i++ {
		m[i] = p.Buttons[i].Code == [3]int{112, 0, carrierUsageBase + i}
	}
	return m
}

// ---------------------------------------------------------------------------
// Runtime status and the platform part

// SoftStatus is shown in the UI.
type SoftStatus struct {
	Enabled   bool            `json:"enabled"`   // the mode is on (Settings.SoftBindings)
	Carrier   [softSlots]bool `json:"carrier"`   // buttons that send F13..F23 now (last write / read)
	Carriable [softSlots]bool `json:"carriable"` // buttons the profile on the mouse can carry
	OnMouse   string          `json:"onMouse"`   // id of the profile on the mouse
	Hook      bool            `json:"hook"`      // the keyboard hook is installed
	Exe       string          `json:"exe"`       // foreground program
	Profile   string          `json:"profile"`   // id of the active soft profile ("" = global only)
	Held      []string        `json:"held"`      // slots currently held (live feedback)
	Last      string          `json:"last"`      // last handled slot
}

// softHost is the platform part (keyboard hook, SendInput, foreground).
type softHost interface {
	status() SoftStatus
	capture(timeout time.Duration) (string, error)
	changed() // settings, profiles or carrier changed
}

// ---------------------------------------------------------------------------
// Resolving

// activeSoftProfile returns the enabled profile that lists exe, or nil.
func activeSoftProfile(sd *SoftData, exe string) *SoftProfile {
	exe = normalizeExe(exe)
	if exe == "" || sd == nil {
		return nil
	}
	for _, p := range sd.Profiles {
		if !p.Enabled {
			continue
		}
		for _, e := range p.Exes {
			if e == exe {
				return p
			}
		}
	}
	return nil
}

// resolveSoft: forced global → profile → global → default.
func resolveSoft(sd *SoftData, p *SoftProfile, slot string) (SoftBinding, string) {
	g, hasG := sd.Global[slot]
	hasG = hasG && g.Kind != SoftInherit
	if hasG && g.Force {
		return g, "global"
	}
	if p != nil {
		if b, ok := p.Bindings[slot]; ok && b.Kind != SoftInherit {
			return b, "profile"
		}
	}
	if hasG {
		return g, "global"
	}
	return SoftBinding{Kind: SoftDefault}, "default"
}

// softOutput is what button i does right now. mouseP is the profile on the
// mouse (its assignment is the default action). ok = false: do nothing.
func softOutput(sd *SoftData, p *SoftProfile, mouseP *Profile, i int) (SoftOut, bool) {
	b, _ := resolveSoft(sd, p, softSlotName(i))
	switch b.Kind {
	case SoftKeys:
		c, err := parseCombo(b.Keys)
		if err != nil {
			return SoftOut{}, false
		}
		return SoftOut{Kind: SoftKeys, Combo: c}, true
	case SoftMouse:
		return SoftOut{Kind: SoftMouse, Button: b.Button}, true
	case SoftDisable:
		return SoftOut{Kind: SoftDisable}, true
	}
	if mouseP == nil {
		return SoftOut{}, false
	}
	return fwOutput(mouseP.Buttons[i])
}

// ---------------------------------------------------------------------------
// Validation

func softSlotLabel(slot string) string {
	return T("кнопка ", "button ") + strings.TrimPrefix(slot, "b")
}

func validSoftMouseButton(s string) bool {
	for _, b := range softMouseButtons {
		if b == s {
			return true
		}
	}
	return false
}

func validateSoftBinding(slot string, b *SoftBinding, global bool) error {
	switch b.Kind {
	case SoftInherit, SoftDefault, SoftDisable:
	case SoftKeys:
		c, err := parseCombo(b.Keys)
		if err != nil {
			return fmt.Errorf("%s: %v", softSlotLabel(slot), err)
		}
		b.Keys = c.String()
	case SoftMouse:
		if !validSoftMouseButton(b.Button) {
			return fmt.Errorf(T("%s: неизвестная кнопка мыши %q", "%s: unknown mouse button %q"), softSlotLabel(slot), b.Button)
		}
	default:
		return fmt.Errorf(T("%s: неизвестное действие %q", "%s: unknown action %q"), softSlotLabel(slot), b.Kind)
	}
	if b.Kind != SoftKeys {
		b.Keys = ""
	}
	if b.Kind != SoftMouse {
		b.Button = ""
	}
	if !global {
		b.Force = false
	}
	return nil
}

func validateSoftBindings(m map[string]SoftBinding, global bool) error {
	for slot, b := range m {
		if softSlotIndex(slot) < 0 {
			delete(m, slot)
			continue
		}
		if err := validateSoftBinding(slot, &b, global); err != nil {
			return err
		}
		if b.Kind == SoftInherit {
			delete(m, slot) // a forced "inherit" means nothing
			continue
		}
		m[slot] = b
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

func validateSoftProfile(p *SoftProfile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New(T("у профиля должно быть имя", "the profile needs a name"))
	}
	exes := []string{}
	seen := map[string]bool{}
	for _, e := range p.Exes {
		e = normalizeExe(e)
		if e == "" || seen[e] {
			continue
		}
		if !strings.HasSuffix(e, ".exe") {
			return fmt.Errorf(T("%q: укажите имя файла программы, например acad.exe", "%q: give the program's file name, e.g. acad.exe"), e)
		}
		seen[e] = true
		exes = append(exes, e)
	}
	p.Exes = exes
	if p.Bindings == nil {
		p.Bindings = map[string]SoftBinding{}
	}
	return validateSoftBindings(p.Bindings, false)
}

// validateSoftData normalises sd in place. Every exe may belong to one
// enabled profile only. Lists are never left nil (the UI expects arrays).
func validateSoftData(sd *SoftData) error {
	if sd.Global == nil {
		sd.Global = map[string]SoftBinding{}
	}
	if sd.Profiles == nil {
		sd.Profiles = []*SoftProfile{}
	}
	if err := validateSoftBindings(sd.Global, true); err != nil {
		return err
	}
	owner := map[string]string{}
	ids := map[string]bool{}
	for _, p := range sd.Profiles {
		if p == nil {
			return errors.New(T("пустой профиль", "empty profile"))
		}
		if err := validateSoftProfile(p); err != nil {
			return fmt.Errorf("%s: %v", p.Name, err)
		}
		if p.ID == "" || ids[p.ID] {
			p.ID = newID("s")
		}
		ids[p.ID] = true
		if !p.Enabled {
			continue
		}
		for _, e := range p.Exes {
			if o, ok := owner[e]; ok {
				return fmt.Errorf(T("%s указан и в «%s», и в «%s»", "%s is listed in both %q and %q"), e, o, p.Name)
			}
			owner[e] = p.Name
		}
	}
	return nil
}
