//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

// restart runs exe, the update just installed, in this console and exits
// with its code: Windows can't replace a running process. It returns an
// error, for this copy to carry on, if exe fails within its first seconds.
func restart(exe string, env []string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdin, os.Stdout, os.Stderr, env
	start := time.Now()
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &exit) && time.Since(start) > 2*time.Second:
		os.Exit(exit.ExitCode()) // it ran and ended badly: its own error showed
	}
	return err
}
