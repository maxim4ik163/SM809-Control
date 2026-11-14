package main

// Folders (groups) for the macro, profile and rule lists. Purely an app-side
// way to organise a long list: an item has a Group name ("" = no folder) and
// the list's folder slice (Data.MacroGroups, ProfileGroups, RuleGroups) keeps
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

// folders is one folder list: the names in display order and the folder
// field of every item.
type folders struct {
	list  *[]string
	items func() []*string
}

func macroFolders(d *Data) folders {
	return folders{&d.MacroGroups, func() []*string {
		r := make([]*string, len(d.Macros))
		for i, m := range d.Macros {
			r[i] = &m.Group
		}
		return r
	}}
}

func profileFolders(d *Data) folders {
	return folders{&d.ProfileGroups, func() []*string {
		r := make([]*string, len(d.Profiles))
		for i, p := range d.Profiles {
			r[i] = &p.Group
		}
		return r
	}}
}

func ruleFolders(d *Data) folders {
	return folders{&d.RuleGroups, func() []*string {
		r := make([]*string, len(d.Rules))
		for i := range d.Rules {
			r[i] = &d.Rules[i].Group
		}
		return r
	}}
}

// syncFolders makes all three folder lists consistent.
func syncFolders(d *Data) {
	macroFolders(d).sync()
	profileFolders(d).sync()
	ruleFolders(d).sync()
}

// index finds a folder by name, ignoring case.
func (f folders) index(name string) int {
	for i, g := range *f.list {
		if strings.EqualFold(g, name) {
			return i
		}
	}
	return -1
}

// sync makes the folder list consistent: never nil, no blanks or duplicates
// (case-insensitive), and every folder an item uses is listed. An item's
// folder name takes the spelling of the listed folder.
func (f folders) sync() {
	groups := make([]string, 0, len(*f.list))
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
	for _, g := range *f.list {
		add(g)
	}
	for _, it := range f.items() {
		*it = add(*it)
	}
	*f.list = groups
}

func (f folders) add(name string) (string, error) {
	name = normGroup(name)
	if name == "" {
		return "", errors.New(T("введите имя папки", "enter a folder name"))
	}
	if f.index(name) >= 0 {
		return "", errors.New(T("такая папка уже есть", "a folder with this name already exists"))
	}
	*f.list = append(*f.list, name)
	return name, nil
}

// rename renames a folder. Renaming onto another existing folder merges the
// two (its items move there, the old folder disappears).
func (f folders) rename(old, name string) (string, error) {
	oi := f.index(old)
	if oi < 0 {
		return "", errors.New(T("папка не найдена", "folder not found"))
	}
	name = normGroup(name)
	if name == "" {
		return "", errors.New(T("введите имя папки", "enter a folder name"))
	}
	l := *f.list
	oldName := l[oi]
	if ni := f.index(name); ni >= 0 && ni != oi {
		name = l[ni]
		*f.list = append(l[:oi], l[oi+1:]...)
	} else {
		l[oi] = name
	}
	for _, it := range f.items() {
		if strings.EqualFold(*it, oldName) {
			*it = name
		}
	}
	return name, nil
}

// remove deletes a folder; its items are kept, without a folder.
func (f folders) remove(name string) error {
	i := f.index(name)
	if i < 0 {
		return errors.New(T("папка не найдена", "folder not found"))
	}
	l := *f.list
	g := l[i]
	*f.list = append(l[:i], l[i+1:]...)
	for _, it := range f.items() {
		if strings.EqualFold(*it, g) {
			*it = ""
		}
	}
	return nil
}

// shift moves a folder one place up (dir < 0) or down (dir > 0).
func (f folders) shift(name string, dir int) error {
	i := f.index(name)
	if i < 0 {
		return errors.New(T("папка не найдена", "folder not found"))
	}
	j := i + 1
	if dir < 0 {
		j = i - 1
	}
	if l := *f.list; j >= 0 && j < len(l) {
		l[i], l[j] = l[j], l[i]
	}
	return nil
}

// op runs a folder operation from the API: add, rename, delete, up, down.
func (f folders) op(op, name, to string) (string, error) {
	switch op {
	case "add":
		return f.add(name)
	case "rename":
		return f.rename(name, to)
	case "delete":
		return name, f.remove(name)
	case "up":
		return name, f.shift(name, -1)
	case "down":
		return name, f.shift(name, 1)
	}
	return "", errors.New(T("неизвестная операция", "unknown operation"))
}

// Macro wrappers (kept for the existing callers and tests).

func groupIndex(d *Data, name string) int                { return macroFolders(d).index(name) }
func syncMacroGroups(d *Data)                            { macroFolders(d).sync() }
func addMacroGroup(d *Data, name string) (string, error) { return macroFolders(d).add(name) }
func renameMacroGroup(d *Data, old, name string) (string, error) {
	return macroFolders(d).rename(old, name)
}
func deleteMacroGroup(d *Data, name string) error         { return macroFolders(d).remove(name) }
func shiftMacroGroup(d *Data, name string, dir int) error { return macroFolders(d).shift(name, dir) }

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

// moveProfileTo puts a profile into a folder ("" = no folder).
func moveProfileTo(d *Data, id, group string) error {
	for _, p := range d.Profiles {
		if p.ID == id {
			p.Group = normGroup(group)
			profileFolders(d).sync()
			return nil
		}
	}
	return errors.New(T("профиль не найден", "profile not found"))
}
