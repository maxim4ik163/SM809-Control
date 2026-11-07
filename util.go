//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func syscallUTF16(s string) ([]uint16, error) { return syscall.UTF16FromString(s) }

var (
	logMu      sync.Mutex
	logEnabled bool
	logFile    string
)

func logf(format string, a ...any) {
	if !logEnabled || logFile == "" {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	if fi, err := os.Stat(logFile); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(logFile, logFile+".old")
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s\r\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, a...))
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(p)
}
