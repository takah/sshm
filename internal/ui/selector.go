package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/takah/sshm/internal/aws"
	"golang.org/x/term"
)

var (
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	headerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
)

type model struct {
	instances []aws.Instance
	filtered  []aws.Instance
	cursor    int
	search    textinput.Model
	selected  *aws.Instance
	quitting  bool
	width     int
	height    int
}

func initialModel(instances []aws.Instance) model {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.Focus()

	return model{
		instances: instances,
		filtered:  instances,
		search:    ti,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.filtered) > 0 {
				m.selected = &m.filtered[m.cursor]
			}
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)

	// Filter instances based on search input
	query := strings.ToLower(m.search.Value())
	m.filtered = nil
	for _, inst := range m.instances {
		text := strings.ToLower(inst.Name + " " + inst.InstanceID + " " + inst.Profile.RoleName + " " + inst.Profile.Name)
		if strings.Contains(text, query) {
			m.filtered = append(m.filtered, inst)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}

	return m, cmd
}

// computeWidths calculates column widths based on terminal width.
// Name is always full width. When space is tight, account is reduced first,
// then permission set (minimum 3), then instance ID (minimum 6).
func (m model) computeWidths() (nameW, idW, accountW, permSetW int) {
	const (
		cursorW    = 2
		sep        = 2
		idIdeal    = 19 // len("i-xxxxxxxxxxxxxxxxx")
		idMin      = 6
		permSetMin = 5
	)

	nameW         = 4  // min "NAME"
	accountIdeal  := 7  // min "ACCOUNT"
	permSetIdeal  := 14 // min "PERMISSION SET"

	for _, inst := range m.instances {
		if l := len(inst.Name); l > nameW {
			nameW = l
		}
		if l := len(inst.Profile.Name); l > accountIdeal {
			accountIdeal = l
		}
		if l := len(inst.Profile.RoleName); l > permSetIdeal {
			permSetIdeal = l
		}
	}

	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 120
	}

	// cursor(2) + 3 separators between 4 columns (2 spaces each) = 2 + 6 = 8
	overhead := cursorW + 3*sep
	available := totalWidth - overhead - nameW
	if available < 0 {
		available = 0
	}

	idW      = idIdeal
	accountW = accountIdeal
	permSetW = permSetIdeal

	if idW+accountW+permSetW <= available {
		return
	}

	// Reduce permSet first (min 3) — most shrinkable
	excess := idW + accountW + permSetW - available
	if permSetW > permSetMin {
		cut := min(excess, permSetW-permSetMin)
		permSetW -= cut
		excess -= cut
		if excess == 0 {
			return
		}
	}

	// Reduce ID to minimum (6)
	if idW > idMin {
		cut := min(excess, idW-idMin)
		idW -= cut
		excess -= cut
		if excess == 0 {
			return
		}
	}

	// Still not enough — hide permSet entirely (account stays full)
	permSetW = 0
	return
}

func (m model) renderRow(name, id, account, permSet string, nameW, idW, accountW, permSetW int) string {
	parts := []string{fmt.Sprintf("%-*s", nameW, name)}
	if idW > 0 {
		parts = append(parts, fmt.Sprintf("%-*s", idW, truncate(id, idW)))
	}
	if accountW > 0 {
		parts = append(parts, fmt.Sprintf("%-*s", accountW, truncate(account, accountW)))
	}
	if permSetW > 0 {
		parts = append(parts, truncate(permSet, permSetW))
	}
	return strings.Join(parts, "  ")
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString(headerStyle.Render("sshm - Select an instance"))
	b.WriteString("\n\n")
	b.WriteString(m.search.View())
	b.WriteString("\n\n")

	nameW, idW, accountW, permSetW := m.computeWidths()

	// Column header
	header := m.renderRow("NAME", "INSTANCE ID", "ACCOUNT", "PERMISSION SET", nameW, idW, accountW, permSetW)
	b.WriteString(dimStyle.Render("  " + header))
	b.WriteString("\n")

	// Show up to 20 items
	start := 0
	visible := 20
	if m.cursor >= visible {
		start = m.cursor - visible + 1
	}

	end := start + visible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	for i := start; i < end; i++ {
		inst := m.filtered[i]
		line := m.renderRow(inst.Name, inst.InstanceID, inst.Profile.Name, inst.Profile.RoleName, nameW, idW, accountW, permSetW)

		if i == m.cursor {
			b.WriteString(selectedStyle.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("\n%s",
		dimStyle.Render(fmt.Sprintf("  %d/%d instances | ↑↓ navigate | enter select | esc quit",
			len(m.filtered), len(m.instances)))))

	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// SelectInstance shows an interactive list and returns the selected instance.
func SelectInstance(instances []aws.Instance) (aws.Instance, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		if state, err := term.GetState(fd); err == nil {
			defer term.Restore(fd, state) //nolint:errcheck
		}
	}

	p := tea.NewProgram(initialModel(instances))
	result, err := p.Run()
	if err != nil {
		return aws.Instance{}, fmt.Errorf("UI error: %w", err)
	}

	m := result.(model)
	if m.selected == nil {
		return aws.Instance{}, fmt.Errorf("no instance selected")
	}
	return *m.selected, nil
}
