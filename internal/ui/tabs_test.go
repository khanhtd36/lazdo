package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTabBarClicksHitTheDrawnTab(t *testing.T) {
	labels := []string{"1 Overview", "2 Files (12)", "3 Commits (3)"}
	for _, width := range []int{120, 30} {
		bar := ansi.Strip(tabBar(labels, 1, width))
		for i, l := range labels {
			x := strings.Index(bar, l)
			if x < 0 {
				t.Fatalf("width %d: %q missing from %q", width, l, bar)
			}
			for _, at := range []int{x, x + len(l) - 1} {
				if got, ok := tabAt(labels, at, width); !ok || got != i {
					t.Errorf("width %d: click at %d = %d %v, want %d", width, at, got, ok, i)
				}
			}
		}
	}
	if ws := tabWidths([]string{"1 PRs"}, 80); ws[0] != tabMinWidth {
		t.Errorf("short label width %d, want %d", ws[0], tabMinWidth)
	}
}
