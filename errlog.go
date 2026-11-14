//go:build windows

package main

// Error log: errors.log next to the settings, written always — whatever the
// "Write a log" setting says — so that a problem that already happened can
// still be looked at. Error messages only, never request data (software
// buttons may hold typed text). Over errLogMax the file becomes errors.log.old.

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const errLogMax = 1 << 20

var (
	errLogMu   sync.Mutex
	errLogFile string // set at start-up; "" = not written (tests)
	errLogLast time.Time
)

// errlogf writes one line to errors.log (and to the main log when it is on).
func errlogf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	logf("ОШИБКА: %s", msg)
	errLogMu.Lock()
	defer errLogMu.Unlock()
	errLogLast = time.Now()
	if errLogFile == "" {
		return
	}
	if fi, err := os.Stat(errLogFile); err == nil && fi.Size() > errLogMax {
		_ = os.Rename(errLogFile, errLogFile+".old")
	}
	f, err := os.OpenFile(errLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	msg = strings.ReplaceAll(strings.TrimRight(msg, "\r\n"), "\n", "\r\n    ")
	fmt.Fprintf(f, "%s  %s %s\r\n", time.Now().Format("2006-01-02 15:04:05.000"), appVersion, msg)
}

// errLogPanic logs a recovered panic with its stack; where says what was running.
func errLogPanic(where string, r any) {
	errlogf("сбой (%s): %v\n%s", where, r, debug.Stack())
}

// lastError: when the last error was logged (this run, or the file's time).
func lastError() time.Time {
	errLogMu.Lock()
	defer errLogMu.Unlock()
	if !errLogLast.IsZero() || errLogFile == "" {
		return errLogLast
	}
	if fi, err := os.Stat(errLogFile); err == nil && fi.Size() > 0 {
		return fi.ModTime()
	}
	return time.Time{}
}
