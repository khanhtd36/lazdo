package ui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func bigFiles(t *testing.T, n int) *detailModel {
	d := fakeFiles(t, 200)
	var lb, rb strings.Builder
	for i := range n {
		fmt.Fprintf(&lb, "func f%d(x int) int { return x + %d } // some comment text here\n", i, i)
		if i%500 == 250 {
			fmt.Fprintf(&rb, "func f%d(x int) int { return x * %d } // changed\n", i, i)
		} else {
			fmt.Fprintf(&rb, "func f%d(x int) int { return x + %d } // some comment text here\n", i, i)
		}
	}
	left, right := lb.String(), rb.String()
	var blocks []ado.LineBlock
	pos := 1
	for i := 250; i < n; i += 500 {
		blocks = append(blocks, ado.LineBlock{ChangeType: "none", OriginalStart: pos, OriginalCount: i + 1 - pos, ModifiedStart: pos, ModifiedCount: i + 1 - pos})
		blocks = append(blocks, ado.LineBlock{ChangeType: "edit", OriginalStart: i + 1, OriginalCount: 1, ModifiedStart: i + 1, ModifiedCount: 1})
		pos = i + 2
	}
	blocks = append(blocks, ado.LineBlock{ChangeType: "none", OriginalStart: pos, OriginalCount: n + 1 - pos, ModifiedStart: pos, ModifiedCount: n + 1 - pos})
	start := time.Now()
	fd := &fileDiff{leftRaw: splitLines(left), rightRaw: splitLines(right), leftHL: highlight("a.go", left), rightHL: highlight("a.go", right)}
	fmt.Fprintf(os.Stderr, "highlight %v\n", time.Since(start))
	fd.sbs = sideBySideLines(blocks, n, n)
	fd.inline = inlineLines(fd.sbs)
	ts := d.files.threads[:0:0]
	for i := range 30 {
		th := d.files.threads[0]
		ctx := *th.ThreadContext
		ctx.RightFileStart = &ado.Position{Line: 100 + i*600, Offset: 1}
		ctx.RightFileEnd = &ado.Position{Line: 100 + i*600, Offset: 5}
		th.ThreadContext = &ctx
		th.ID = 100 + i
		ts = append(ts, th)
	}
	d.files.threads = ts
	d.files.diffs[d.diffKey(d.selectedChange())] = fd
	d.rowCaches = map[string]*diffRows{}
	return d
}

func TestZZPerfBigDiff(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip()
	}
	d := bigFiles(t, 20000)
	timeIt := func(name string, f func()) {
		start := time.Now()
		f()
		fmt.Fprintf(os.Stderr, "%-24s %v\n", name, time.Since(start))
	}
	timeIt("first view", func() { _ = d.view() })
	timeIt("100× j + view", func() {
		for range 100 {
			press(d, "j")
			_ = d.view()
		}
	})
	timeIt("G + view", func() { press(d, "G"); _ = d.view() })
	timeIt("g g + view", func() { press(d, "g", "g"); _ = d.view() })
	timeIt("20× n + view", func() {
		for range 20 {
			press(d, "n")
			_ = d.view()
		}
	})
	timeIt("ctrl+d ×20 + view", func() {
		for range 20 {
			press(d, "ctrl+d")
			_ = d.view()
		}
	})
	timeIt("S (mode) + view", func() { press(d, "S"); _ = d.view() })
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(os.Stderr, "live heap %d MB\n", ms.HeapAlloc>>20)
	runtime.KeepAlive(d)
}
