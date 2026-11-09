package main

// Macro folders (groups). Purely an app-side way to organise a long macro
// list: a macro has a Group name ("" = no folder) and Data.MacroGroups keeps
// the folder order and folders that are still empty. Nothing here is written
// to the mouse.

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const maxGroupName = 40

// normGroup trims a folder name, collapses inner whitespace and limits length.
func normGroup(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxGroupName {
		s = string([]rune(s)[:maxGroupName])
	}
	return s
}

// groupIndex finds a folder by name, ignoring case.
func groupIndex(d *Data, name string) int {
	for i, g := range d.MacroGroups {
		if strings.EqualFold(g, name) {
			return i
		}
	}
	return -1
}

// syncMacroGroups makes the folder list consistent: never nil, no blanks or
// duplicates (case-insensitive), and every folder a macro uses is listed.
// A macro's folder name takes the spelling of the listed folder.
func syncMacroGroups(d *Data) {
	groups := make([]string, 0, len(d.MacroGroups))
	seen := map[string]bool{}
	add := func(g string) string {
		g = normGroup(g)
		if g == "" {
			return ""
		}
		k := strings.ToLower(g)
		if !seen[k] {
			seen[k] = true
			groups = append(groups, g)
			return g
		}
		for _, x := range groups {
			if strings.EqualFold(x, g) {
				return x
			}
		}
		return g
	}
	for _, g := range d.MacroGroups {
		add(g)
	}
	for _, m := range d.Macros {
		m.Group = add(m.Group)
	}
	d.MacroGroups = groups
}

func addMacroGroup(d *Data, name string) (string, error) {
	name = normGroup(name)
	if name == "" {
		return "", errors.New(T("введите имя папки", "enter a folder name"))
	}
	if i := groupIndex(d, name); i >= 0 {
		return "", errors.New(T("такая папка уже есть", "a folder with this name already exists"))
	}
	d.MacroGroups = append(d.MacroGroups, name)
	return name, nil
}

// renameMacroGroup renames a folder. Renaming onto another existing folder
// merges the two (its macros move there, the old folder disappears).
func renameMacroGroup(d *Data, old, name string) (string, error) {
	oi := groupIndex(d, old)
	if oi < 0 {
		return "", errors.New(T("папка не найдена", "folder not found"))
	}
	name = normGroup(name)
	if name == "" {
		return "", errors.New(T("введите имя папки", "enter a folder name"))
	}
	oldName := d.MacroGroups[oi]
	if ni := groupIndex(d, name); ni >= 0 && ni != oi {
		name = d.MacroGroups[ni]
		d.MacroGroups = append(d.MacroGroups[:oi], d.MacroGroups[oi+1:]...)
	} else {
		d.MacroGroups[oi] = name
	}
	for _, m := range d.Macros {
		if strings.EqualFold(m.Group, oldName) {
			m.Group = name
		}
	}
	return name, nil
}

// deleteMacroGroup removes a folder; its macros are kept, without a folder.
func deleteMacroGroup(d *Data, name string) error {
	i := groupIndex(d, name)
	if i < 0 {
		return errors.New(T("папка не найдена", "folder not found"))
	}
	g := d.MacroGroups[i]
	d.MacroGroups = append(d.MacroGroups[:i], d.MacroGroups[i+1:]...)
	for _, m := range d.Macros {
		if strings.EqualFold(m.Group, g) {
			m.Group = ""
		}
	}
	return nil
}

// shiftMacroGroup moves a folder one place up (dir < 0) or down (dir > 0).
func shiftMacroGroup(d *Data, name string, dir int) error {
	i := groupIndex(d, name)
	if i < 0 {
		return errors.New(T("папка не найдена", "folder not found"))
	}
	j := i + 1
	if dir < 0 {
		j = i - 1
	}
	if j >= 0 && j < len(d.MacroGroups) {
		d.MacroGroups[i], d.MacroGroups[j] = d.MacroGroups[j], d.MacroGroups[i]
	}
	return nil
}

// moveMacroTo puts a macro into a folder ("" = no folder). An unknown folder
// name creates the folder.
func moveMacroTo(d *Data, id, group string) error {
	var m *Macro
	for _, x := range d.Macros {
		if x.ID == id {
			m = x
		}
	}
	if m == nil {
		return errors.New(T("макрос не найден", "macro not found"))
	}
	m.Group = normGroup(group)
	syncMacroGroups(d)
	return nil
}
