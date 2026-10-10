package sysmem

import "testing"

func TestTotal(t *testing.T) {
	if n := Total(); n < 256<<20 {
		t.Fatalf("Total() = %d bytes, want at least 256 MB", n)
	}
}
