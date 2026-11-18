package tui

import (
	"encoding/base64"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Text selection: positions address the transcript content (logical line +
// display column), never the screen, so a selection anchor survives
// scrolling and resizing. The left button sets the anchor; drag and wheel
// events move the head. Scrolling with the button held extends the selection
// across the newly revealed lines, like a native terminal.

// mouseWheelStep matches the viewport's default wheel delta (lines per tick).
const mouseWheelStep = 3

// Viewport origin inside the rendered frame: the border box draws one border
// row above the title, and one border column at x=0.
const (
	vpOriginX = 1
	vpOriginY = 2
)

type selPos struct {
	line int // zero-based index into m.lines
	col  int // display-cell column
}

type selection struct {
	active bool // left button currently held
	shown  bool // a selection is displayed (persists after release)
	anchor selPos
	head   selPos
}

func selLess(a, b selPos) bool {
	if a.line != b.line {
		return a.line < b.line
	}
	return a.col < b.col
}

// clampSelPos bounds a position to the current transcript.
func (m *Model) clampSelPos(p selPos) selPos {
	if len(m.lines) == 0 {
		return selPos{}
	}
	if p.line < 0 {
		return selPos{}
	}
	if p.line >= len(m.lines) {
		p.line = len(m.lines) - 1
	}
	if p.col < 0 {
		p.col = 0
	}
	return p
}

// vpPos maps a mouse event to a transcript position, reporting whether the
// event landed inside the transcript viewport.
func (m *Model) vpPos(x, y int) (selPos, bool) {
	vx, vy := x-vpOriginX, y-vpOriginY
	if vx < 0 || vy < 0 || vx >= m.vp.Width || vy >= m.vp.Height {
		return selPos{}, false
	}
	return m.clampSelPos(selPos{line: m.vp.YOffset + vy, col: vx}), true
}

// vpPosClamped maps a mouse event to a transcript position, clamping events
// that fall outside the viewport onto its nearest edge (drag behavior).
func (m *Model) vpPosClamped(x, y int) selPos {
	vx, vy := x-vpOriginX, y-vpOriginY
	if vx < 0 {
		vx = 0
	}
	if vy < 0 {
		vy = 0
	}
	if vy > m.vp.Height-1 {
		vy = m.vp.Height - 1
	}
	return m.clampSelPos(selPos{line: m.vp.YOffset + vy, col: vx})
}

// handleMouse implements selection and wheel scrolling.
func (m *Model) handleMouse(msg tea.MouseMsg) {
	switch {
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		if p, ok := m.vpPos(msg.X, msg.Y); ok {
			m.sel = selection{active: true, shown: true, anchor: p, head: p}
		} else {
			m.sel = selection{} // clicking outside the transcript clears it
		}

	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionMotion:
		if m.sel.active {
			m.sel.head = m.vpPosClamped(msg.X, msg.Y)
		}

	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionRelease:
		if m.sel.active {
			// keep the current head (drag or wheel extended); the
			// release position itself is not the selection endpoint
			m.sel.active = false
			copySelection(m.selectionText())
		}

	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		m.vp.ScrollUp(mouseWheelStep)
		if m.sel.active {
			// extend the selection through the lines the scroll revealed
			m.sel.head = m.clampSelPos(selPos{line: m.vp.YOffset, col: 0})
		}

	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		m.vp.ScrollDown(mouseWheelStep)
		if m.sel.active {
			m.sel.head = m.clampSelPos(selPos{line: m.vp.YOffset + m.vp.Height - 1, col: 1 << 24})
		}
	}
	m.refreshView()
}

// selBounds returns the normalized (lo, hi) selection positions.
func (m *Model) selBounds() (lo, hi selPos, ok bool) {
	if !m.sel.shown || len(m.lines) == 0 {
		return selPos{}, selPos{}, false
	}
	lo, hi = m.sel.anchor, m.sel.head
	if selLess(hi, lo) {
		lo, hi = hi, lo
	}
	lo = m.clampSelPos(lo)
	hi = m.clampSelPos(hi)
	if lo.line == hi.line && lo.col == hi.col {
		return selPos{}, selPos{}, false
	}
	return lo, hi, true
}

// renderContent builds the viewport content from the transcript lines,
// applying reverse video to the selection.
func (m *Model) renderContent() string {
	lo, hi, ok := m.selBounds()
	if !ok {
		return strings.Join(m.lines, "\n")
	}
	var b strings.Builder
	for i, line := range m.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch {
		case i < lo.line || i > hi.line:
			b.WriteString(line)
		case i == lo.line && i == hi.line:
			b.WriteString(highlightLine(line, lo.col, hi.col))
		case i == lo.line:
			b.WriteString(highlightLine(line, lo.col, ansi.StringWidth(line)))
		case i == hi.line:
			b.WriteString(highlightLine(line, 0, hi.col))
		default:
			b.WriteString(highlightLine(line, 0, ansi.StringWidth(line)))
		}
	}
	return b.String()
}

// selectionText extracts the plain text covered by the selection.
func (m *Model) selectionText() string {
	lo, hi, ok := m.selBounds()
	if !ok {
		return ""
	}
	var b strings.Builder
	for i := lo.line; i <= hi.line; i++ {
		line := stripANSI(m.lines[i])
		w := ansi.StringWidth(line)
		c0, c1 := 0, w
		if i == lo.line && lo.col > c0 {
			c0 = min(lo.col, w)
		}
		if i == hi.line && hi.col < c1 {
			c1 = min(hi.col, w)
		}
		seg := strings.TrimRight(ansi.Cut(line, c0, c1), " \t")
		b.WriteString(seg)
		if i < hi.line {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// highlightLine renders line with the display-column range [c0, c1) in
// reverse video, preserving the line's own colors.
func highlightLine(line string, c0, c1 int) string {
	w := ansi.StringWidth(line)
	if c0 < 0 {
		c0 = 0
	}
	if c1 > w {
		c1 = w
	}
	if c1 <= c0 {
		return line
	}
	head, body, tail := ansi.Cut(line, 0, c0), ansi.Cut(line, c0, c1), ansi.Cut(line, c1, w)
	// Inner resets (lipgloss emits \x1b[0m) would drop reverse video
	// mid-selection; re-apply it after each reset.
	if strings.Contains(body, "\x1b[0m") {
		body = strings.ReplaceAll(body, "\x1b[0m", "\x1b[0m\x1b[7m")
	}
	return head + "\x1b[7m" + body + "\x1b[0m" + tail
}

// stripANSI removes escape sequences, leaving printable text.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		if j >= len(s) {
			break
		}
		switch s[j] {
		case '[': // CSI: skip to the final byte
			j++
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
		case ']': // OSC: skip to BEL or ST
			j++
			for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			if j < len(s) && s[j] == 0x1b {
				j += 2
			} else if j < len(s) {
				j++
			}
		default: // two-character escape
			j++
		}
		i = j
	}
	return b.String()
}

// copySelection emits an OSC 52 clipboard write so the selection can be
// pasted outside the terminal. Raw OSC 52 works both directly and inside
// tmux (tmux's set-clipboard intercepts it and re-emits it outward).
func copySelection(text string) {
	if text == "" {
		return
	}
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	os.Stdout.WriteString(seq)
}
