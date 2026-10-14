package ui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func heapMB() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc >> 20
}

func TestZZPerfHeap(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip()
	}
	base := heapMB()
	var b strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&b, "func f%d(x int) int { return x + %d } // some comment text here\n", i, i)
	}
	src := b.String()
	raw := splitLines(src)
	fmt.Fprintf(os.Stderr, "text %d MB, raw lines +%d MB\n", len(src)>>20, heapMB()-base)
	before := heapMB()
	hl := highlight("a.go", src)
	fmt.Fprintf(os.Stderr, "one side highlighted +%d MB\n", heapMB()-before)
	n := 0
	for i := range hl.len() {
		l := hl.segs(i)
		n += len(l)
	}
	fmt.Fprintf(os.Stderr, "segs %d (%.1f per line)\n", n, float64(n)/float64(hl.len()))
	runtime.KeepAlive(hl)
	runtime.KeepAlive(raw)
	d := bigFiles(t, 20000)
	fmt.Fprintf(os.Stderr, "whole diff model +%d MB\n", heapMB()-before)
	runtime.KeepAlive(d)
}

func TestZZPerfBrowser(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip()
	}
	b := fakeBrowser(t)
	var sb strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&sb, "func f%d(x int) int { return x + %d } // some comment text here\n", i, i)
	}
	src := sb.String()
	b.file, b.pane = "/main.go", paneContent
	b.contents[folderKey(b.branch, b.file)] = &fileContent{raw: splitLines(src), hl: highlight("main.go", src)}
	before := heapMB()
	start := time.Now()
	_ = b.view(200, 40)
	fmt.Fprintf(os.Stderr, "browser first view %v, +%d MB\n", time.Since(start), heapMB()-before)
	start = time.Now()
	for range 100 {
		b.key(keyMsg("j"), 200, 40)
		_ = b.view(200, 40)
	}
	fmt.Fprintf(os.Stderr, "browser 100× j %v\n", time.Since(start))
	start = time.Now()
	b.key(keyMsg("G"), 200, 40)
	_ = b.view(200, 40)
	fmt.Fprintf(os.Stderr, "browser G %v\n", time.Since(start))
	runtime.KeepAlive(b)
}
