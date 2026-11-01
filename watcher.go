//go:build windows

package main

import "time"

// watchProcesses polls the process list; the engine reacts only to
// starts/exits of executables that have rules.
func watchProcesses(e *Engine, st *Store) {
	for {
		e.onProcesses(runningProcesses())
		st.mu.Lock()
		n := st.data.Settings.ScanSeconds
		st.mu.Unlock()
		if n < 1 {
			n = 2
		}
		time.Sleep(time.Duration(n) * time.Second)
	}
}
