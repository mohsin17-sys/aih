package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
)

func newTestModel(nLines, height int) *Model {
	m := &Model{vp: viewport.New(40, height)}
	for i := 0; i < nLines; i++ {
		m.lines = append(m.lines, "line "+strings.Repeat("x", i))
	}
	m.refreshView()
	return m
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[38;5;42mhello\x1b[0m world\x1b]0;title\x07end"
	if got := stripANSI(in); got != "hello worldend" {
		t.Fatalf("stripANSI = %q", got)
	}
}

func TestHighlightLine(t *testing.T) {
	line := "\x1b[31mred\x1b[0m plain"
	got := highlightLine(line, 0, 5) // "red p"
	if !strings.HasPrefix(got, "\x1b[7m") {
		t.Fatalf("selection not reversed at start: %q", got)
	}
	if !strings.Contains(got, "\x1b[0m\x1b[7m") {
		t.Fatalf("inner reset did not re-apply reverse: %q", got)
	}
	if highlightLine(line, 5, 3) != line {
		t.Fatal("empty range must leave the line unchanged")
	}
}

func TestSelectionTextAcrossLines(t *testing.T) {
	m := newTestModel(10, 5)
	m.sel = selection{shown: true,
		anchor: selPos{line: 1, col: 0},
		head:   selPos{line: 3, col: 2},
	}
	// line 1 = "line x", line 2 = "line xx", line 3 = "line xxx"
	want := "line x\nline xx\nli"
	if got := m.selectionText(); got != want {
		t.Fatalf("selectionText = %q, want %q", got, want)
	}
}

func TestRenderContentHighlightsSelection(t *testing.T) {
	m := newTestModel(5, 5)
	m.sel = selection{shown: true,
		anchor: selPos{line: 1, col: 0},
		head:   selPos{line: 2, col: 3},
	}
	out := m.renderContent()
	if !strings.Contains(out, "\x1b[7m") {
		t.Fatal("renderContent missing reverse video")
	}
	lines := strings.Split(out, "\n")
	if lines[0] != m.lines[0] || lines[3] != m.lines[3] {
		t.Fatal("unselected lines must be unchanged")
	}
}

func TestWheelDuringDragExtendsSelection(t *testing.T) {
	m := newTestModel(100, 10)
	// view is at the bottom (YOffset = 90); press on visible row 5
	m.handleMouse(tea.MouseMsg{X: 5, Y: 5 + vpOriginY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.sel.active || m.sel.anchor.line != 95 {
		t.Fatalf("anchor = %+v, want line 95", m.sel.anchor)
	}
	// wheel up while holding: view scrolls 3 and selection extends to the new top row
	m.handleMouse(tea.MouseMsg{X: 5, Y: 5 + vpOriginY, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.vp.YOffset != 87 {
		t.Fatalf("YOffset = %d, want 87", m.vp.YOffset)
	}
	if m.sel.head.line != 87 || m.sel.head.col != 0 {
		t.Fatalf("head = %+v, want line 87 col 0", m.sel.head)
	}
	got := m.selectionText()
	wantFirst, wantLast := "line "+strings.Repeat("x", 87), "line"
	if !strings.HasPrefix(got, wantFirst+"\n") {
		t.Fatalf("selection does not start at wheel-revealed line 87: %q", got[:90])
	}
	if !strings.HasSuffix(got, wantLast) {
		t.Fatal("selection does not end at the drag anchor (line 95, cut at col 4)")
	}

	// repeated wheel-ups reach the top and the selection covers everything
	for i := 0; i < 40; i++ {
		m.handleMouse(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	}
	if m.vp.YOffset != 0 || m.sel.head.line != 0 {
		t.Fatalf("top: YOffset=%d head=%+v", m.vp.YOffset, m.sel.head)
	}
	if n := strings.Count(m.selectionText(), "\n"); n != 95 {
		t.Fatalf("selection spans %d newlines, want 95 (lines 0..95)", n)
	}

	// release clears the active drag but keeps the highlight
	m.handleMouse(tea.MouseMsg{X: 5, Y: 5, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.sel.active {
		t.Fatal("release must end the drag")
	}
	if !strings.Contains(m.renderContent(), "\x1b[7m") {
		t.Fatal("selection highlight lost after release")
	}
}

func TestDragClampsToViewport(t *testing.T) {
	m := newTestModel(20, 5)
	m.vp.YOffset = 0
	m.sel.active = true // pin the view at the top (no GotoBottom on refresh)
	m.handleMouse(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.sel.anchor.line != 0 {
		t.Fatalf("anchor = %+v", m.sel.anchor)
	}
	// drag far below the viewport: clamps to the last visible row
	m.handleMouse(tea.MouseMsg{X: 5, Y: 40, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if m.sel.head.line != 4 {
		t.Fatalf("head = %+v, want line 4", m.sel.head)
	}
}

func TestClickOutsideClearsSelection(t *testing.T) {
	m := newTestModel(20, 5)
	m.sel = selection{shown: true, anchor: selPos{line: 1, col: 0}, head: selPos{line: 2, col: 3}}
	m.handleMouse(tea.MouseMsg{X: 50, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.sel.shown {
		t.Fatal("click outside the transcript must clear the selection")
	}
}
