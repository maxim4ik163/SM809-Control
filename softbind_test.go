package main

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// Carrier layout of the default SM809 profile. The expected packets were
// computed independently from docs/PROTOCOL.md (cmd 1, sub 2 / 18, button
// order 11,12,13,14,15,9,10,0,1 | 2,3,4,5,8,6,7, pad FF to 30 bytes, checksum
// (6 + Σ) & FF inserted at index 3), seq 0.
func TestCarrierPackets(t *testing.T) {
	p := newProfile("t", 0)
	want := [16]bool{}
	for i := 0; i < 11; i++ {
		want[i] = i != 9 // button 10 = rapid fire stays on the mouse
	}
	m := carriedMask(p)
	for i := 0; i < softSlots; i++ {
		if m[i] != want[i] {
			t.Fatalf("carried[%d] = %v, want %v", i, m[i], want[i])
		}
	}
	c := carrierProfile(p)
	var codes [numButtons]KeyCode
	for i, b := range c.Buttons {
		codes[i] = KeyCode{byte(b.Code[0]), byte(b.Code[1]), byte(b.Code[2])}
	}
	pk := EncodeKeys(codes, func() byte { return 0 })
	exp := []string{
		"0100029a100100100200100400400200400300301002700072700068700069",
		"0100121e70006a70006b70006c70006d70007070006e70006fffffffffffff",
	}
	for i := range exp {
		if got := hex.EncodeToString(pk[i]); got != exp[i] {
			t.Errorf("packet %d:\n got %s\nwant %s", i, got, exp[i])
		}
	}
	// the source profile is untouched
	if p.Buttons[0].Code != [3]int{112, 8, 7} {
		t.Fatal("carrierProfile changed its argument")
	}
	if carrierInMemory(c) != m {
		t.Fatal("carrierInMemory does not recognise the carrier layout")
	}
	if carrierInMemory(p) != [softSlots]bool{} {
		t.Fatal("normal layout seen as carrier")
	}
	// buttons 12..16 never change
	for i := 11; i < 16; i++ {
		if c.Buttons[i] != p.Buttons[i] {
			t.Fatalf("button %d changed", i+1)
		}
	}
}

func TestDeviceProfileSignature(t *testing.T) {
	p := newProfile("t", 0)
	sig := func(soft bool) map[string]string {
		m := map[string]string{}
		for _, c := range buildCommands(deviceProfile(p, soft), func(string) *Macro { return nil }, 0x11) {
			m[c.key] = signature(c)
		}
		return m
	}
	a, b := sig(false), sig(true)
	for k := range a {
		if (a[k] != b[k]) != (k == "keys") {
			t.Errorf("%s: soft mode should change only the keys command", k)
		}
	}
	// a remap between two carried actions does not change what is written
	p.Buttons[3] = ButtonAssign{Kind: "mouse", Code: [3]int{16, 8, 0}} // forward → back
	if sig(true)["keys"] != b["keys"] {
		t.Error("carried → carried change needs no write in soft mode")
	}
	// leaving the carrier (a macro) does
	p.Buttons[3] = ButtonAssign{Kind: "macro", Code: [3]int{144, 0, 0}, MacroID: "m"}
	if sig(true)["keys"] == b["keys"] {
		t.Error("carried → firmware-only change must be written")
	}
}

func TestFwOutput(t *testing.T) {
	cases := []struct {
		code [3]int
		kind string
		want string // combo or mouse button; "" = not carried
	}{
		{[3]int{112, 8, 7}, SoftKeys, "Win+D"},
		{[3]int{112, 1, 6}, SoftKeys, "Ctrl+C"},
		{[3]int{112, 5, 0}, SoftKeys, "Ctrl+Alt"}, // modifiers only
		{[3]int{112, 0, 31}, SoftKeys, "2"},
		{[3]int{112, 0, 88}, SoftKeys, "NumEnter"},
		{[3]int{112, 0, 104}, SoftKeys, "F13"},
		{[3]int{112, 0, 229}, SoftKeys, "Shift"},
		{[3]int{112, 0, 250}, "", ""}, // unknown usage
		{[3]int{128, 148, 1}, SoftKeys, "MyComputer"},
		{[3]int{128, 233, 0}, SoftKeys, "VolumeUp"},
		{[3]int{128, 131, 1}, SoftKeys, "MediaPlayer"},
		{[3]int{128, 1, 2}, "", ""},
		{[3]int{16, 16, 0}, SoftMouse, "forward"},
		{[3]int{16, 8, 0}, SoftMouse, "back"},
		{[3]int{16, 2, 0}, SoftMouse, "right"},
		{[3]int{16, 1, 0}, "", ""}, // the left click is never carried
		{[3]int{96, 0, 0}, SoftDisable, ""},
		{[3]int{96, 4, 0}, "", ""},  // lighting toggle
		{[3]int{64, 2, 0}, "", ""},  // DPI +
		{[3]int{80, 3, 0}, "", ""},  // DPI lock
		{[3]int{48, 50, 3}, "", ""}, // rapid fire
	}
	for _, c := range cases {
		o, ok := fwOutput(ButtonAssign{Kind: "x", Code: c.code})
		if ok != (c.kind != "") {
			t.Errorf("%v: carried = %v", c.code, ok)
			continue
		}
		if !ok {
			continue
		}
		got := o.Button
		if o.Kind == SoftKeys {
			got = o.Combo.String()
		}
		if o.Kind != c.kind || got != c.want {
			t.Errorf("%v: got %s %q, want %s %q", c.code, o.Kind, got, c.kind, c.want)
		}
	}
	if _, ok := fwOutput(ButtonAssign{Kind: "macro", Code: [3]int{112, 0, 4}}); ok {
		t.Error("a macro must stay on the mouse")
	}
}

// Every key the Buttons tab offers (web/keys.js KEY_GROUPS) must translate.
func TestHIDUsageCoverage(t *testing.T) {
	var usages []int
	for u := 4; u <= 57; u++ {
		if u != 50 {
			usages = append(usages, u)
		}
	}
	for u := 58; u <= 101; u++ {
		usages = append(usages, u)
	}
	for u := 104; u <= 115; u++ {
		usages = append(usages, u)
	}
	for u := 224; u <= 231; u++ {
		usages = append(usages, u)
	}
	for _, u := range usages {
		if _, ok := fwOutput(ButtonAssign{Code: [3]int{112, 0, u}}); !ok {
			t.Errorf("usage %d does not translate", u)
		}
	}
	for c, n := range consumerName {
		if _, err := parseCombo(n); err != nil {
			t.Errorf("consumer %#x → %q: %v", c, n, err)
		}
	}
}

func TestResolveSoft(t *testing.T) {
	sd := defaultSoftData()
	sd.Global["b1"] = SoftBinding{Kind: SoftKeys, Keys: "Ctrl+C"}
	sd.Global["b2"] = SoftBinding{Kind: SoftDisable, Force: true}
	p := &SoftProfile{Name: "cad", Exes: []string{"C:\\Prog\\ACAD.EXE"}, Enabled: true, Bindings: map[string]SoftBinding{
		"b1":  {Kind: SoftMouse, Button: "middle"},
		"b2":  {Kind: SoftKeys, Keys: "Esc"},
		"b3":  {Kind: SoftKeys, Keys: "z+ctrl", Force: true},
		"b99": {Kind: SoftDisable},
	}}
	sd.Profiles = append(sd.Profiles, p)
	if err := validateSoftData(sd); err != nil {
		t.Fatal(err)
	}
	if p.Exes[0] != "acad.exe" || p.Bindings["b3"].Keys != "Ctrl+Z" || p.Bindings["b3"].Force {
		t.Fatalf("not normalised: %+v", p)
	}
	if _, ok := p.Bindings["b99"]; ok {
		t.Fatal("invalid slot kept")
	}
	ap := activeSoftProfile(sd, "acad.exe")
	if ap != p || activeSoftProfile(sd, "notepad.exe") != nil {
		t.Fatal("activeSoftProfile")
	}
	mouseP := newProfile("m", 0)
	out := func(prof *SoftProfile, i int) string {
		o, ok := softOutput(sd, prof, mouseP, i)
		if !ok {
			return "-"
		}
		switch o.Kind {
		case SoftKeys:
			return o.Combo.String()
		case SoftMouse:
			return "mouse:" + o.Button
		}
		return o.Kind
	}
	checks := []struct {
		prof *SoftProfile
		i    int
		want string
	}{
		{nil, 0, "Ctrl+C"},        // global
		{ap, 0, "mouse:middle"},   // profile over global
		{ap, 1, "disable"},        // forced global over profile
		{ap, 2, "Ctrl+Z"},         // profile only
		{nil, 2, "1"},             // default = mouse profile (button 3 = "1")
		{nil, 3, "mouse:forward"}, // default
		{nil, 9, "-"},             // rapid fire: firmware only, nothing to do
	}
	for _, c := range checks {
		if got := out(c.prof, c.i); got != c.want {
			t.Errorf("button %d: got %s, want %s", c.i+1, got, c.want)
		}
	}
	// a disabled profile is not active
	p.Enabled = false
	if activeSoftProfile(sd, "acad.exe") != nil {
		t.Fatal("disabled profile active")
	}
}

func TestValidateSoftData(t *testing.T) {
	sd := &SoftData{}
	if err := validateSoftData(sd); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(sd)
	if strings.Contains(string(b), "null") {
		t.Fatalf("null in JSON: %s", b) // the UI breaks on null lists
	}
	bad := []*SoftData{
		{Global: map[string]SoftBinding{"b1": {Kind: SoftKeys, Keys: "Ctrl+Nope"}}},
		{Global: map[string]SoftBinding{"b1": {Kind: SoftMouse, Button: "side"}}},
		{Global: map[string]SoftBinding{"b1": {Kind: "macro"}}},
		{Profiles: []*SoftProfile{{Name: "x", Exes: []string{"notepad"}}}},
		{Profiles: []*SoftProfile{{Name: " "}}},
		{Profiles: []*SoftProfile{{Name: "a", Exes: []string{"x.exe"}, Enabled: true}, {Name: "b", Exes: []string{"X.EXE"}, Enabled: true}}},
	}
	for i, sd := range bad {
		if err := validateSoftData(sd); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
	// the same exe in a disabled profile is fine
	ok := &SoftData{Profiles: []*SoftProfile{{Name: "a", Exes: []string{"x.exe"}, Enabled: true}, {Name: "b", Exes: []string{"x.exe"}}}}
	if err := validateSoftData(ok); err != nil {
		t.Fatal(err)
	}
	if ok.Profiles[0].ID == "" || ok.Profiles[0].ID == ok.Profiles[1].ID {
		t.Fatal("profile ids")
	}
}

func TestSoftSlots(t *testing.T) {
	for i := 0; i < softSlots; i++ {
		if softSlotIndex(softSlotName(i)) != i || carrierSlotOfVK(carrierVK(i)) != i {
			t.Fatal(i)
		}
	}
	for _, s := range []string{"b0", "b12", "b01", "x1", "b", ""} {
		if softSlotIndex(s) != -1 {
			t.Errorf("%q accepted", s)
		}
	}
	if carrierSlotOfVK(0x7B) != -1 || carrierSlotOfVK(0x87) != -1 {
		t.Error("F12 / F24 are not carrier keys")
	}
	k, ok := keyByVK[0x7C]
	if !ok || k.Name != "F13" {
		t.Error("VK_F13")
	}
}
