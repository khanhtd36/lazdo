package ui

import (
	"strings"
	"testing"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestSideBySideAndInlineLines(t *testing.T) {
	blocks := []ado.LineBlock{
		{ChangeType: "none", OriginalStart: 1, OriginalCount: 2, ModifiedStart: 1, ModifiedCount: 2},
		{ChangeType: "edit", OriginalStart: 3, OriginalCount: 1, ModifiedStart: 3, ModifiedCount: 2},
		{ChangeType: "delete", OriginalStart: 4, OriginalCount: 1, ModifiedStart: 5, ModifiedCount: 0},
		{ChangeType: "none", OriginalStart: 5, OriginalCount: 1, ModifiedStart: 5, ModifiedCount: 1},
	}
	sbs := sideBySideLines(blocks, 5, 5)
	want := []diffLine{
		{lineContext, 1, 1},
		{lineContext, 2, 2},
		{lineEdit, 3, 3},
		{lineEdit, 0, 4},
		{lineDelete, 4, 0},
		{lineContext, 5, 5},
	}
	if len(sbs) != len(want) {
		t.Fatalf("sbs: got %v", sbs)
	}
	for i := range want {
		if sbs[i] != want[i] {
			t.Fatalf("sbs[%d]: got %v, want %v", i, sbs[i], want[i])
		}
	}
	in := inlineLines(sbs)
	wantIn := []diffLine{
		{lineContext, 1, 1},
		{lineContext, 2, 2},
		{lineDelete, 3, 0},
		{lineDelete, 4, 0},
		{lineAdd, 0, 3},
		{lineAdd, 0, 4},
		{lineContext, 5, 5},
	}
	if len(in) != len(wantIn) {
		t.Fatalf("inline: got %v", in)
	}
	for i := range wantIn {
		if in[i] != wantIn[i] {
			t.Fatalf("inline[%d]: got %v, want %v", i, in[i], wantIn[i])
		}
	}
}

func TestNextChange(t *testing.T) {
	c, a := diffLine{kind: lineContext}, diffLine{kind: lineAdd}
	lines := []diffLine{c, a, a, c, c, a, c}
	for _, tc := range []struct{ from, dir, want int }{
		{0, 1, 1}, {1, 1, 5}, {2, 1, 5}, {5, 1, 5}, // no further change: stay
		{6, -1, 5}, {5, -1, 1}, {4, -1, 1}, {1, -1, 1},
	} {
		if got := nextChange(lines, tc.from, tc.dir); got != tc.want {
			t.Errorf("nextChange(from %d, dir %d) = %d, want %d", tc.from, tc.dir, got, tc.want)
		}
	}
}

func TestWholeFileLines(t *testing.T) {
	added := sideBySideLines(nil, 0, 2)
	if len(added) != 2 || added[0] != (diffLine{lineAdd, 0, 1}) {
		t.Fatalf("added file: %v", added)
	}
}

func TestHighlightKeepsLines(t *testing.T) {
	src := "package main\n\n/* multi\nline */\nfunc main() {\n\tx := \"a\"\n}\n"
	lines := highlight("main.go", src)
	if len(lines) != 7 {
		t.Fatalf("got %d lines, want 7", len(lines))
	}
	var b strings.Builder
	for _, s := range lines[5] {
		b.WriteString(s.text)
	}
	if b.String() != "    x := \"a\"" {
		t.Fatalf("line 6 = %q", b.String())
	}
}

func TestWrapSegs(t *testing.T) {
	rows := wrapSegs([]seg{{text: "abcd"}, {text: "efgh", fg: "#fff"}}, 3)
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		var b strings.Builder
		for _, s := range r {
			b.WriteString(s.text)
		}
		got = append(got, b.String())
	}
	if strings.Join(got, "|") != "abc|def|gh" {
		t.Fatalf("got %v", got)
	}
}
