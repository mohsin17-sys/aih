package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"aiharness/internal/agent"
	"aiharness/internal/app"
	"aiharness/internal/providers"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	borderStyle    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("42"))
	titleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	resultStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	jevStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	errStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	inputStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	hintStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	thinkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

const (
	sidePanelWidth = 32
	maxSteps       = 40
)

// agentEventMsg wraps one streamed agent event.
type agentEventMsg agent.Event

// agentDoneMsg signals the agent loop finished.
type agentDoneMsg struct{ err error }

type Model struct {
	width, height int
	input         textinput.Model
	vp            viewport.Model
	transcript    string // copy-safe (plain string, appended by reassignment)

	ag     *agent.Agent      // pointer: shared, never copied by value
	events chan agent.Event // channel: copy-safe reference

	thinking     bool
	jevCalls     int
	editorCalls  int
	toolCalls    int
	filesChanged []string
	clipWidth    int
	ready        bool
	quitting     bool
}

// New builds the TUI model with a live agent for the given working dir.
func New(wd string) (Model, error) {
	ag, err := app.NewAgent(wd)
	if err != nil {
		return Model{}, err
	}

	ti := textinput.New()
	ti.Placeholder = "ask aih anything…"
	ti.Prompt = "❯ "
	ti.CharLimit = 0
	ti.Focus()

	return Model{
		input:  ti,
		vp:     viewport.New(0, 0),
		ag:     ag,
		events: make(chan agent.Event, 64),
	}, nil
}

func (m Model) Init() tea.Cmd { return textinput.Blink }

// waitEvent returns a command that blocks until the next agent event.
func waitEvent(ch chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentEventMsg(ev)
	}
}

// startAgent runs the agent loop in the background until it completes.
func (m Model) startAgent() tea.Cmd {
	ag := m.ag
	return func() tea.Msg {
		err := ag.Run(context.Background(), maxSteps)
		return agentDoneMsg{err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		return m, nil

	case agentEventMsg:
		m.renderEvent(agent.Event(msg))
		// keep draining while the loop may still be running (or buffered)
		if m.thinking || len(m.events) > 0 {
			return m, waitEvent(m.events)
		}
		return m, nil

	case agentDoneMsg:
		m.thinking = false
		m.input.Focus()
		if msg.err != nil {
			m.appendTranscript(errStyle.Render("✗ agent error: "+firstLine(msg.err.Error())) + "\n\n")
		}
		if len(m.events) > 0 { // drain anything that raced the done signal
			return m, waitEvent(m.events)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			if m.thinking {
				return m, nil // input locked while agent runs
			}
			line := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			if line == "" {
				return m, nil
			}
			if line == "exit" || line == "quit" {
				m.quitting = true
				return m, tea.Quit
			}
			// append user turn and launch the loop
			m.ag.Messages = append(m.ag.Messages, providers.Message{Role: "user", Content: line})
			m.appendTranscript(userStyle.Render("you: ")+clip(line, maxInt(10, m.clipWidth)) + "\n")
			m.thinking = true
			m.input.Blur()
			// wire Emit once, on the pointer, before the run
			m.ag.Emit = func(ev agent.Event) { m.events <- ev }
			return m, tea.Batch(m.startAgent(), waitEvent(m.events))
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// renderEvent styles one agent event into the transcript and updates counters.
func (m *Model) renderEvent(ev agent.Event) {
	switch ev.Type {
	case "tool_call":
		m.toolCalls++
		switch ev.Tool {
		case "jev_route":
			m.jevCalls++
		case "generate_edit":
			m.editorCalls++
		case "write_file":
			if p, ok := ev.Args["file_path"].(string); ok {
				m.filesChanged = appendUnique(m.filesChanged, p)
			}
		}
		m.appendTranscript(toolStyle.Render("→ "+ev.Tool+" "+clip(marshalArgs(ev.Args), maxInt(10, m.clipWidth-4))) + "\n")

	case "tool_result":
		style := resultStyle
		prefix := "← "
		if ev.Tool == "jev_route" {
			style = jevStyle
		}
		m.appendTranscript(style.Render(prefix+clip(firstLine(ev.Result), maxInt(10, m.clipWidth-2))) + "\n")

	case "error":
		m.appendTranscript(errStyle.Render("✗ "+ev.Tool+": "+clip(firstLine(ev.Result), maxInt(10, m.clipWidth-2))) + "\n")

	case "assistant":
		m.appendTranscript(assistantStyle.Render(clip(ev.Content, 4*m.clipWidth)) + "\n\n")
	}
}

// appendTranscript adds styled content and refreshes the viewport.
func (m *Model) appendTranscript(styled string) {
	m.transcript += styled
	m.vp.SetContent(m.transcript)
	m.vp.GotoBottom()
}

// layout sizes the viewport after a resize.
func (m *Model) layout() {
	leftWidth := m.width - sidePanelWidth - 4
	if leftWidth < 20 {
		leftWidth = 20
	}
	vpHeight := m.height - 6
	if vpHeight < 3 {
		vpHeight = 3
	}
	m.vp.Width = leftWidth - 2
	m.vp.Height = vpHeight
	m.clipWidth = m.vp.Width - 8 // room for "you: " prefix and margin
	m.vp.GotoBottom()
}

func (m Model) View() string {
	if m.quitting {
		return "bye.\n"
	}
	if !m.ready {
		return "loading…"
	}

	left := borderStyle.
		Width(m.vp.Width + 2).
		Height(m.vp.Height + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(" AIH "),
			m.vp.View(),
		))

	right := borderStyle.
		Width(sidePanelWidth).
		Height(m.vp.Height + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(" Context "),
			strings.Join(m.sideLines(), "\n"),
		))

	inputRow := borderStyle.
		Width(m.width - 2).
		Render(inputStyle.Render("  " + m.input.View()))

	statusLeft := "mistral-large-latest ▸ qwen-14b@mlx"
	if m.thinking {
		statusLeft = thinkStyle.Render("⏳ thinking…")
	}
	status := statusStyle.Render(" " + statusLeft) +
		strings.Repeat(" ", maxInt(1, m.width-lipgloss.Width(statusLeft)-24)) +
		hintStyle.Render("ctrl+c exit · session 2")

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right),
		inputRow,
		status,
	)
}

// sideLines builds the live context panel.
func (m Model) sideLines() []string {
	lines := []string{
		fmt.Sprintf("Tool calls:  %d", m.toolCalls),
		fmt.Sprintf("Jev calls:   %d", m.jevCalls),
		fmt.Sprintf("Editor calls:%d", m.editorCalls),
		"",
		fmt.Sprintf("Files changed: %d", len(m.filesChanged)),
	}
	for _, f := range m.filesChanged {
		lines = append(lines, "  "+clip(f, sidePanelWidth-4))
	}
	return padLines(lines, "  ")
}

// ---- small helpers ----

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func marshalArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(b)
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

func padLines(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = prefix + l
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Run starts the TUI program with a live agent for the current directory.
func Run() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	model, err := New(wd)
	if err != nil {
		return err
	}
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
