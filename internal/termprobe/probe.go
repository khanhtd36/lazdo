// Package termprobe asks the terminal how wide it draws some text: it
// prints the text, asks where the cursor went (a cursor position report),
// and erases the line again. Fonts and East Asian width settings decide
// whether symbols like ● take one column or two, and only the terminal knows.
package termprobe

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// Width reports how many columns the terminal drew s in. ok is false when
// stdin or stdout isn't a terminal or the terminal didn't answer in time.
func Width(s string, timeout time.Duration) (width int, ok bool) {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return 0, false
	}
	state, err := term.MakeRaw(in)
	if err != nil {
		return 0, false
	}
	defer func() { _ = term.Restore(in, state) }()
	restoreOut := enableVT(os.Stdout)
	defer restoreOut()

	fmt.Fprint(os.Stdout, "\r"+s+"\x1b[6n")
	defer fmt.Fprint(os.Stdout, "\r\x1b[2K") // leave no trace

	var reply []byte
	buf := make([]byte, 64)
	deadline := time.Now().Add(timeout)
	for !bytes.Contains(reply, []byte("R")) {
		left := time.Until(deadline)
		if left <= 0 || !readable(os.Stdin, left) {
			return 0, false
		}
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0, false
		}
		reply = append(reply, buf[:n]...)
	}
	return parseColumn(reply)
}

// parseColumn reads the column from the last "ESC [ row ; col R" in b; the
// cursor started in column 1, so the text took col-1 columns.
func parseColumn(b []byte) (int, bool) {
	i := bytes.LastIndex(b, []byte("\x1b["))
	if i < 0 {
		return 0, false
	}
	var row, col int
	if _, err := fmt.Sscanf(string(b[i:]), "\x1b[%d;%dR", &row, &col); err != nil || col < 1 {
		return 0, false
	}
	return col - 1, true
}
