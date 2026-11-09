package main

import (
	"os"
	"testing"
)

// Needs the vendor updater locally (not in the repo): IAP_ORIG=path go test
func TestPatch(t *testing.T) {
	p := os.Getenv("IAP_ORIG")
	if p == "" {
		t.Skip("IAP_ORIG not set")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := patch(b); err != nil {
		t.Fatal(err)
	}
	b[100] ^= 1
	if _, err := patch(b); err == nil {
		t.Fatal("modified input accepted")
	}
}
