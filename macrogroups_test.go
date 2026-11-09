package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMacroGroups(t *testing.T) {
	d := &Data{Macros: []*Macro{
		{ID: "a", Group: "  Raid   macros "},
		{ID: "b", Group: "raid MACROS"},
		{ID: "c"},
		{ID: "d", Group: "PvP"},
	}, MacroGroups: []string{"Empty", "", "empty"}}
	syncMacroGroups(d)
	if want := []string{"Empty", "Raid macros", "PvP"}; !reflect.DeepEqual(d.MacroGroups, want) {
		t.Fatalf("groups %q", d.MacroGroups)
	}
	if d.Macros[0].Group != "Raid macros" || d.Macros[1].Group != "Raid macros" || d.Macros[2].Group != "" {
		t.Fatalf("macro groups %q %q", d.Macros[0].Group, d.Macros[1].Group)
	}

	if _, err := addMacroGroup(d, "pvp"); err == nil {
		t.Fatal("duplicate folder accepted")
	}
	if _, err := addMacroGroup(d, "   "); err == nil {
		t.Fatal("blank folder accepted")
	}
	if g, err := addMacroGroup(d, strings.Repeat("я", 60)); err != nil || len([]rune(g)) != maxGroupName {
		t.Fatalf("long name %q %v", g, err)
	}

	// rename keeps macros, rename onto another folder merges
	if _, err := renameMacroGroup(d, "pvp", "Arena"); err != nil || d.Macros[3].Group != "Arena" {
		t.Fatalf("rename %v %q", err, d.Macros[3].Group)
	}
	if _, err := renameMacroGroup(d, "Arena", "raid macros"); err != nil || d.Macros[3].Group != "Raid macros" || groupIndex(d, "Arena") >= 0 {
		t.Fatalf("merge %v %q %q", err, d.Macros[3].Group, d.MacroGroups)
	}

	// order
	_ = shiftMacroGroup(d, "Raid macros", -1)
	if d.MacroGroups[0] != "Raid macros" {
		t.Fatalf("up %q", d.MacroGroups)
	}
	_ = shiftMacroGroup(d, "Raid macros", -1) // already first: no change
	if d.MacroGroups[0] != "Raid macros" {
		t.Fatalf("up at top %q", d.MacroGroups)
	}

	// delete keeps the macros, without a folder
	if err := deleteMacroGroup(d, "RAID MACROS"); err != nil || len(d.Macros) != 4 || d.Macros[0].Group != "" || d.Macros[3].Group != "" {
		t.Fatalf("delete %v", err)
	}

	// move into a new folder creates it; "" takes the macro out
	if err := moveMacroTo(d, "c", " Farm "); err != nil || d.Macros[2].Group != "Farm" || groupIndex(d, "Farm") < 0 {
		t.Fatalf("move %v %q", err, d.MacroGroups)
	}
	if err := moveMacroTo(d, "c", ""); err != nil || d.Macros[2].Group != "" || groupIndex(d, "Farm") < 0 {
		t.Fatal("move out (the folder stays)")
	}
	if moveMacroTo(d, "zzz", "x") == nil {
		t.Fatal("unknown macro moved")
	}

	// lists are never null in JSON; the group is omitted when empty
	e := &Data{}
	syncMacroGroups(e)
	b, _ := json.Marshal(e)
	if !strings.Contains(string(b), `"macroGroups":[]`) {
		t.Fatalf("null list: %s", b)
	}
	b, _ = json.Marshal(Macro{ID: "x"})
	if strings.Contains(string(b), "group") {
		t.Fatalf("empty group serialised: %s", b)
	}
}

// The folder is app-only: it must not change what is sent to the mouse.
func TestMacroGroupNotEncoded(t *testing.T) {
	m := Macro{ID: "x", ExecuteType: 2, RepeatCount: 1, Events: []MacroEvent{{Type: "keydown", Code: 4, Delay: 10}, {Type: "keyup", Code: 4, Index: 1}}}
	g := m
	g.Group = "Folder"
	if !reflect.DeepEqual(EncodeMacroEvents(m), EncodeMacroEvents(g)) {
		t.Fatal("group changes the macro bytes")
	}
}
