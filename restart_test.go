package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestartFailureLetsThisCopyCarryOn(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "lazdo-new")
	if err := restart(missing, os.Environ()); err == nil {
		t.Fatal("a copy that can't start returns its error instead of exiting")
	}
}
