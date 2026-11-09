package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Data model (persisted as JSON)

// ButtonAssign is what a physical button does. Code is the 3-byte firmware
// action; for macros Code is derived at apply time from the macro settings.
type ButtonAssign struct {
	Kind    string `json:"kind"` // mouse, key, media, dpi, dpilock, fire, special, macro
	Code    [3]int `json:"code"`
	Label   string `json:"label"`
	MacroID string `json:"macroId,omitempty"`
}

type Profile struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Buttons [16]ButtonAssign `json:"buttons"`

	DPICount  int       `json:"dpiCount"`  // 1..6
	DPILevels [8]int    `json:"dpiLevels"` // DPI values
	DPIColors [8]string `json:"dpiColors"` // #rrggbb
	DPIStart  int       `json:"dpiStart"`  // level selected when the profile is applied

	PollRate     int  `json:"pollRate"` // firmware code: 8,4,2,1,17,18,20
	LOD          int  `json:"lod"`      // 1 or 2 mm
	SleepMin     int  `json:"sleepMin"` // 1..20
	MoveWakeup   bool `json:"moveWakeup"`
	MoveLightOff bool `json:"moveLightOff"`
	AngleSnap    bool `json:"angleSnap"`
	MotionSync   bool `json:"motionSync"`
	Ripple       bool `json:"ripple"`

	Lighting int `json:"lighting"` // 0..10

	// Soft: actions of buttons 1..11 when they are software buttons
	// (software mode); Kind "" = nothing.
	Soft [softSlots]SoftBinding `json:"soft"`
}

type Rule struct {
	Exe       string `json:"exe"` // lower-case file name, e.g. enshrouded.exe
	ProfileID string `json:"profileId"`
	OSD       bool   `json:"osd"`
	Enabled   bool   `json:"enabled"`
}

type Settings struct {
	DefaultProfile string `json:"defaultProfile"`
	BaseProfile    string `json:"baseProfile"` // chosen manually; profile used when no rule is active

	OSDProfile bool `json:"osdProfile"` // show popup on profile switch (per-rule flag can disable)
	OSDDPI     bool `json:"osdDpi"`

	// AutoSwitch enables switching profiles by running programs. Off by default:
	// every switch rewrites the mouse's flash, so the user has to opt in.
	AutoSwitch bool `json:"autoSwitch"`

	Popup PopupStyle `json:"popup"`

	LowPercent      int  `json:"lowPercent"`
	CriticalPercent int  `json:"criticalPercent"`
	NotifyFull      bool `json:"notifyFull"`
	NotifyLink      bool `json:"notifyLink"`

	ScanSeconds int  `json:"scanSeconds"` // process watcher interval
	Debug       bool `json:"debug"`

	Language string `json:"language"`

	// Experimental: key debounce threshold (firmware command 9). Only sent
	// when DebounceSet; factory value is 2.
	DebounceSet bool `json:"debounceSet"`
	Debounce    int  `json:"debounce"` // "ru" or "en"; chosen from the system language on first run

	BTPaths []string `json:"btPaths"` // remembered Bluetooth HID paths → BT1, BT2

	// ButtonMode: "" (not chosen yet: the UI asks), ModeHardware or
	// ModeSoftware (see softbind.go). Changed only through /api/buttons/mode.
	ButtonMode string `json:"buttonMode"`
	// HWButtons: in software mode, buttons 1..11 that stay hardware buttons.
	// Changed only through /api/buttons/hw.
	HWButtons [softSlots]bool `json:"hwButtons"`
	// KeyRepeat: typematic repeat for a software button sending a single key.
	KeyRepeat bool `json:"keyRepeat"`
	// RestoreOnExit writes the hardware layout back when the app exits (one
	// flash write per exit, and another one at the next start).
	RestoreOnExit bool `json:"restoreOnExit"`

	SoftBindings bool `json:"softBindings,omitempty"` // 1.5 betas, migrated
}

type PopupStyle struct {
	Position     string `json:"position"`
	OffsetX      int    `json:"offsetX"`
	OffsetY      int    `json:"offsetY"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	CornerRadius int    `json:"cornerRadius"`
	Background   string `json:"background"`
	Opacity      int    `json:"opacity"`
	TextColor    string `json:"textColor"`
	CaptionColor string `json:"captionColor"`
	AccentColor  string `json:"accentColor"`
	DotColor     string `json:"dotColor"`
	Font         string `json:"font"`
	FontSize     int    `json:"fontSize"`
	CaptionSize  int    `json:"captionSize"`
	Bold         bool   `json:"bold"`
	DurationMs   int    `json:"durationMs"`
	FadeMs       int    `json:"fadeMs"`
	UseDPIColors bool   `json:"useDpiColors"` // accent = colour of the DPI level
}

type Data struct {
	Version  int        `json:"version"`
	Profiles []*Profile `json:"profiles"`
	Macros   []*Macro   `json:"macros"`
	Rules    []Rule     `json:"rules"`
	Settings Settings   `json:"settings"`
	Soft     *SoftData  `json:"soft,omitempty"` // 1.5 betas, migrated into profiles
}

// dataVersion 2 (SM809 Control 2.0): button modes, Profile.Soft.
// 3 (2.1): every button but the left click can be a software button.
const dataVersion = 3

// DeviceState is what we last wrote to the mouse (signatures per command), so
// that only differences are sent. Not part of user data.
type DeviceState struct {
	Synced      bool              `json:"synced"` // a full profile has been written at least once
	Seeded      bool              `json:"seeded"` // default profile was initialised from the mouse status
	Seq         int               `json:"seq"`    // last command sequence number used
	Sent        map[string]string `json:"sent"`
	FlashWrites int               `json:"flashWrites"`
	LastProfile string            `json:"lastProfile"`
	LastApplied time.Time         `json:"lastApplied"`
	// Carrier: buttons 1..11 that send F13..F23 according to the last keys
	// write (or the last memory read). The keyboard hook handles only these.
	Carrier [softSlots]bool `json:"carrier"`
	// CarrierVer: version of the carrier key table used for that write.
	CarrierVer int `json:"carrierVer,omitempty"`
}

// setCarrier records which buttons carry F-keys and tells the hook.
// Must be called with Store.mu held.
func (s *Store) setCarrier(m [softSlots]bool) {
	s.state.Carrier = m
	s.state.CarrierVer = carrierTableVersion
	if onCarrierChange != nil {
		onCarrierChange(m)
	}
}

// onCarrierChange is set on Windows (keyboard hook); nil in tests.
var onCarrierChange func([softSlots]bool)

func (d *DeviceState) lastSeq() int {
	if d.Seq < 1 || d.Seq > 255 {
		return 1
	}
	return d.Seq
}

// ---------------------------------------------------------------------------
// Defaults (SM809 template from the vendor web driver)

var lightingModes = []string{
	"Выключено", "Мерцание звёзд", "Colorful streaming", "Семицветное растяжение",
	"Семицветное мигание", "Цветное дыхание", "Семицветные вспышки", "Colorful coverage",
	"Красный", "Зелёный", "Синий",
}

func defaultButtons() [16]ButtonAssign {
	b := func(kind, label string, c ...int) ButtonAssign {
		return ButtonAssign{Kind: kind, Label: label, Code: [3]int{c[0], c[1], c[2]}}
	}
	return [16]ButtonAssign{
		b("key", "Win+D", 112, 8, 7),
		b("key", "2", 112, 0, 31),
		b("key", "1", 112, 0, 30),
		b("mouse", "Вперёд", 16, 16, 0),
		b("key", "4", 112, 0, 33),
		b("key", "3", 112, 0, 32),
		b("key", "6", 112, 0, 35),
		b("key", "5", 112, 0, 34),
		b("mouse", "Назад", 16, 8, 0),
		b("fire", "Огонь (16 мс, 2)", 48, 16, 2),
		b("media", "Мой компьютер", 128, 148, 1),
		b("mouse", "Левая кнопка", 16, 1, 0),
		b("mouse", "Правая кнопка", 16, 2, 0),
		b("mouse", "Средняя кнопка", 16, 4, 0),
		b("dpi", "DPI +", 64, 2, 0),
		b("dpi", "DPI −", 64, 3, 0),
	}
}

var defaultDPIColors = [8]string{"#ff0000", "#0000ff", "#00ff00", "#ffff00", "#00ffff", "#ff00ff", "#ff6b6b", "#4ecdc4"}

func newProfile(name string, sensorType int) *Profile {
	si := sensorFor(sensorType)
	p := &Profile{
		ID:         newID("p"),
		Name:       name,
		Buttons:    defaultButtons(),
		DPICount:   6,
		DPIColors:  defaultDPIColors,
		DPIStart:   1,
		PollRate:   1, // 1000 Hz
		LOD:        2,
		SleepMin:   20,
		MoveWakeup: true,
		MotionSync: true,
		Lighting:   2,
	}
	for i := range p.DPILevels {
		if i < len(si.Default) {
			p.DPILevels[i] = si.Default[i]
		} else {
			p.DPILevels[i] = si.Default[len(si.Default)-1]
		}
	}
	return p
}

func defaultPopup() PopupStyle {
	return PopupStyle{
		Position: "bottom-center", OffsetY: 120, Width: 240, Height: 120, CornerRadius: 18,
		Background: "#1e1f22", Opacity: 225, TextColor: "#ffffff", CaptionColor: "#9aa0a6",
		AccentColor: "#4caf50", DotColor: "#4a4d52", Font: "Segoe UI", FontSize: 40, CaptionSize: 13,
		Bold: true, DurationMs: 1200, FadeMs: 350, UseDPIColors: true,
	}
}

func defaultData() *Data {
	p := newProfile(T("Основной", "Default"), 0)
	return &Data{
		Version:  dataVersion,
		Profiles: []*Profile{p},
		Settings: Settings{
			DefaultProfile: p.ID, BaseProfile: p.ID,
			OSDProfile: true, OSDDPI: true, Popup: defaultPopup(), KeyRepeat: true,
			LowPercent: 20, CriticalPercent: 10, NotifyFull: true,
			ScanSeconds: 2,
		},
	}
}

// ---------------------------------------------------------------------------
// Persistence

type Store struct {
	mu    sync.Mutex
	dir   string
	data  *Data
	state *DeviceState
}

var idCounter int64

func newID(prefix string) string {
	idCounter++
	return prefix + strconv.FormatInt(time.Now().UnixNano()/1000+idCounter, 36)
}

func appDataDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	d := filepath.Join(base, "SM809Control")
	_ = os.MkdirAll(d, 0755)
	return d
}

func loadStore(dir string) *Store {
	s := &Store{dir: dir}
	s.data = defaultData()
	if b, err := os.ReadFile(filepath.Join(dir, "data.json")); err == nil {
		var d Data
		if err := json.Unmarshal(b, &d); err == nil && len(d.Profiles) > 0 {
			s.data = &d
		} else {
			_ = os.WriteFile(filepath.Join(dir, "data.broken.json"), b, 0644)
		}
	}
	s.state = &DeviceState{Sent: map[string]string{}}
	if b, err := os.ReadFile(filepath.Join(dir, "device.json")); err == nil {
		_ = json.Unmarshal(b, s.state)
		if s.state.Sent == nil {
			s.state.Sent = map[string]string{}
		}
	}
	s.fixup()
	return s
}

func (s *Store) fixup() {
	d := s.data
	if d.Settings.Popup.Width == 0 {
		d.Settings.Popup = defaultPopup()
	}
	if !d.Settings.DebounceSet {
		d.Settings.Debounce = debounceFactory
	}
	if d.Settings.ScanSeconds < 1 {
		d.Settings.ScanSeconds = 2
	}
	if s.profile(d.Settings.DefaultProfile) == nil {
		d.Settings.DefaultProfile = d.Profiles[0].ID
	}
	if s.profile(d.Settings.BaseProfile) == nil {
		d.Settings.BaseProfile = d.Settings.DefaultProfile
	}
	for i := range d.Rules {
		d.Rules[i].Exe = lower(strings.TrimSpace(d.Rules[i].Exe))
	}
	if d.Version < 2 {
		// 2.0: key repeat on by default; 1.5 beta data into profiles and rules
		if d.Soft == nil {
			d.Settings.KeyRepeat = true
		}
		migrateBeta(d, s.state.Carrier, s.profile(d.Settings.BaseProfile))
	}
	if d.Version < 3 {
		// 2.1: buttons 12..16 can be software buttons; they stay hardware ones
		for i := lockedSlot; i < softSlots; i++ {
			d.Settings.HWButtons[i] = true
		}
	}
	d.Version = dataVersion
	d.Settings.HWButtons[lockedSlot] = true
	d.Soft = nil
	switch d.Settings.ButtonMode {
	case "", ModeHardware, ModeSoftware:
	default:
		d.Settings.ButtonMode = ""
	}
	for _, p := range d.Profiles {
		if validateProfileSoft(p) != nil { // damaged file: drop the software actions only
			p.Soft = [softSlots]SoftBinding{}
		}
	}
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) saveData() error  { return writeJSONAtomic(filepath.Join(s.dir, "data.json"), s.data) }
func (s *Store) saveState() error { return writeJSONAtomic(filepath.Join(s.dir, "device.json"), s.state) }

func (s *Store) profile(id string) *Profile {
	for _, p := range s.data.Profiles {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (s *Store) macro(id string) *Macro {
	for _, m := range s.data.Macros {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Validation

func validateProfile(p *Profile, sensorType int) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New(T("у профиля должно быть имя", "the profile needs a name"))
	}
	if p.DPICount < 1 || p.DPICount > maxDPILevels {
		return fmt.Errorf(T("число уровней DPI должно быть от 1 до %d", "number of DPI levels must be 1..%d"), maxDPILevels)
	}
	si := sensorFor(sensorType)
	for i := 0; i < p.DPICount; i++ {
		v := p.DPILevels[i]
		if v < si.Step*2 || v > si.MaxDPI || v%si.Step != 0 {
			return fmt.Errorf(T("DPI уровня %d: %d — допустимо %d..%d с шагом %d", "DPI level %d: %d — allowed %d..%d in steps of %d"), i+1, v, si.Step*2, si.MaxDPI, si.Step)
		}
	}
	if p.DPIStart < 0 || p.DPIStart >= p.DPICount {
		p.DPIStart = 0
	}
	switch p.PollRate {
	case 8, 4, 2, 1, 17, 18, 20:
	default:
		return errors.New(T("неизвестная частота опроса", "unknown polling rate"))
	}
	if p.LOD != 1 && p.LOD != 2 {
		return errors.New(T("LOD должен быть 1 или 2 мм", "LOD must be 1 or 2 mm"))
	}
	if p.SleepMin < 1 || p.SleepMin > 20 {
		return errors.New(T("время сна 1..20 минут", "sleep time must be 1..20 minutes"))
	}
	if p.Lighting < 0 || p.Lighting >= len(lightingModes) {
		return errors.New(T("неизвестный режим подсветки", "unknown lighting mode"))
	}
	hasLeft := false
	for i, b := range p.Buttons {
		for _, c := range b.Code {
			if c < 0 || c > 255 {
				return fmt.Errorf(T("кнопка %d: неверный код", "button %d: invalid code"), i+1)
			}
		}
		if b.Kind == "macro" && b.MacroID == "" {
			return fmt.Errorf(T("кнопка %d: макрос не выбран", "button %d: no macro selected"), i+1)
		}
		if b.Code == [3]int{16, 1, 0} {
			hasLeft = true
		}
	}
	if !hasLeft {
		return errors.New(T("хотя бы одна кнопка должна оставаться левым кликом", "at least one button must stay a left click"))
	}
	return nil
}

func lower(s string) string { return strings.ToLower(s) }
