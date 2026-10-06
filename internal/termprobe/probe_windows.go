package termprobe

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// readable waits up to timeout for console input, so a terminal that never
// answers leaves no read blocked on stdin.
func readable(f *os.File, timeout time.Duration) bool {
	ev, err := windows.WaitForSingleObject(windows.Handle(f.Fd()), uint32(timeout.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0
}

// enableVT turns on escape sequence processing for the console, which the
// cursor position query needs, and returns how to undo it.
func enableVT(f *os.File) func() {
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
