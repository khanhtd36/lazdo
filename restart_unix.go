//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restart replaces this process with exe, the update just installed, in
// the same terminal; it returns only if that fails.
func restart(exe string, env []string) error {
	return syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), env)
}
