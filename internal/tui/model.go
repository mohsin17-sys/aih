package tui

import (
    "strings"

    "github.com/charmbracelet/bubbles/textinput"
    "github.com/charmbracelet/bubbles/viewport"
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

var (
    // theme: dark-green terminal taste
    borderStyle    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("42"))
    titleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
    userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
    assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
    toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
    resultStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
    jevStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
    inputStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
    statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
    hintStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

const sidePanelWidth = 32

type Model struct {
    width, height int
    input         textinput.Model
    vp            viewport.Model
    transcript    string         // accumulated transcript (immutable copy-safe)
    sideLines     []string
    statusLeft    string
    ready         bool
	clipWidth    int
    quitting      bool
}

func New() Model {
    ti := textinput.New()
    ti.Placeholder = "ask aih anything…"
    ti.Prompt = "❯ "
    ti.CharLimit = 0
    ti.Focus()

    vp := viewport.New(0, 0)
    m := Model{
        input:      ti,
        vp:         vp,
        sideLines:  seedSidePanel(),
        statusLeft: "mistral-large-latest ▸ qwen-14b@mlx",
    }
    m.transcript = seedTranscript()
    m.vp.SetContent(m.transcript)
    return m
}

// clip truncates plain (unstyled) s to at most n runes for clean pane
// rendering. Always clip BEFORE applying styles — slicing styled strings
// mid-ANSI-code corrupts terminal output.
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

func seedTranscript() string {
    var b strings.Builder
    b.WriteString(userStyle.Render("you: ") + "create the test report\n\n")
    b.WriteString(toolStyle.Render(`→ read_file {"path":"go.mod"}`) + "\n")
    b.WriteString(resultStyle.Render("← module github.com/omnimonitor/omnimonitor") + "\n\n")
    b.WriteString(toolStyle.Render(`→ jev_route {"query":"verify local…` + "}") + "\n")
    b.WriteString(jevStyle.Render("← Jev selected: filesystem (0.98)") + "\n\n")
    b.WriteString(toolStyle.Render(`→ write_file {"path":"harness_repo…` + "}") + "\n")
    b.WriteString(resultStyle.Render("← wrote 3312 bytes") + "\n\n")
    b.WriteString(assistantStyle.Render("Report created. (static preview; wiring: session 2)") + "\n")
    return b.String()
}

func seedSidePanel() []string {
    return []string{
        "Files changed: 1",
        "  harness_report.md +128",
        "",
        "Context: 18.4K / 128K",
        "████████░░░░  14%",
        "",
        "Cost:     $0.021",
        "Jev calls:    3",
        "Editor calls: 1 (Mac)",
    }
}

func (m Model) Init() tea.Cmd { return textinput.Blink }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        m.ready = true
        m.layout()
        return m, nil

    case tea.KeyMsg:
        switch msg.Type {
        case tea.KeyCtrlC, tea.KeyEsc:
            m.quitting = true
            return m, tea.Quit
        case tea.KeyEnter:
            line := strings.TrimSpace(m.input.Value())
            if line != "" {
                if line == "exit" || line == "quit" {
                    m.quitting = true
                    return m, tea.Quit
                }
                m.echoUser(line)
            }
            m.input.SetValue("")
            return m, nil
        }
    }

    var cmd tea.Cmd
    m.input, cmd = m.input.Update(msg)
    m.vp, cmd = m.vp.Update(msg)
    return m, cmd
}

// echoUser appends the user's line to the transcript (session-1 behavior:
// input is visible but not yet sent to the agent).
func (m *Model) echoUser(line string) {
    m.appendTranscript(
        userStyle.Render("you: ") + clip(line, maxInt(10, m.clipWidth)) + "\n" +
            hintStyle.Render("  (agent wiring arrives in session 2)") + "\n")
}

// appendTranscript adds styled content and refreshes the viewport.
func (m *Model) appendTranscript(styled string) {
    m.transcript += styled
    m.vp.SetContent(m.transcript)
    m.vp.GotoBottom()
}

// layout sizes the viewport after a resize.
func (m *Model) layout() {
    leftWidth := m.width - sidePanelWidth - 4 // borders + gap
    if leftWidth < 20 {
        leftWidth = 20
    }
    vpHeight := m.height - 6 // input box + status bar + borders
    if vpHeight < 3 {
        vpHeight = 3
    }
    m.vp.Width = leftWidth - 2
	m.clipWidth = m.vp.Width - 8 // room for "you: " prefix and safety margin
    m.vp.Height = vpHeight
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

    side := make([]string, len(m.sideLines))
    for i, l := range m.sideLines {
        side[i] = "  " + l
    }
    right := borderStyle.
        Width(sidePanelWidth).
        Height(m.vp.Height + 2).
        Render(lipgloss.JoinVertical(lipgloss.Left,
            titleStyle.Render(" Context "),
            strings.Join(side, "\n"),
        ))

    inputRow := borderStyle.
        Width(m.width - 2).
        Render(inputStyle.Render("  " + m.input.View()))

    status := statusStyle.Render(" "+m.statusLeft) +
        strings.Repeat(" ", maxInt(1, m.width-lipgloss.Width(m.statusLeft)-24)) +
        hintStyle.Render("ctrl+c exit · session 1")

    return lipgloss.JoinVertical(lipgloss.Left,
        lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right),
        inputRow,
        status,
    )
}

func maxInt(a, b int) int {
    if a > b {
        return a
    }
    return b
}

// Run starts the TUI program.
func Run() error {
    p := tea.NewProgram(New(), tea.WithAltScreen())
    _, err := p.Run()
    return err
}

