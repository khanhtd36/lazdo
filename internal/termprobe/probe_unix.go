//go:build !windows

package termprobe

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readable waits up to timeout for input, so a terminal that never answers
// leaves no read blocked on stdin.
func readable(f *os.File, timeout time.Duration) bool {
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, int(timeout.Milliseconds()))
	return err == nil && n > 0
}

// enableVT is a no-op: Unix terminals always interpret escape sequences.
func enableVT(*os.File) func() { return func() {} }
