package termprobe

import "testing"

func TestParseColumn(t *testing.T) {
	for in, want := range map[string]int{
		"\x1b[12;6R":       5,
		"j\x1b[3;11R":      10, // a key typed before the reply
		"\x1b[1;1R":        0,
		"\x1b[1;2R\x1b[1;": -1, // cut short: the last report is incomplete
	} {
		got, ok := parseColumn([]byte(in))
		if want < 0 {
			if ok {
				t.Errorf("%q: want no column, got %d", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q: got %d %v, want %d", in, got, ok, want)
		}
	}
}
