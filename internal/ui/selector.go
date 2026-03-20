package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/takah/sshm/internal/aws"
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
		text := strings.ToLower(inst.Name + " " + inst.InstanceID + " " + inst.PrivateIP + " " + inst.Profile.Name)
		if strings.Contains(text, query) {
			m.filtered = append(m.filtered, inst)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}

	return m, cmd
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

	// Column header
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %-40s %-22s %-16s %-14s %s",
		"NAME", "INSTANCE ID", "PRIVATE IP", "TYPE", "PROFILE")))
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
		line := fmt.Sprintf("%-40s %-22s %-16s %-14s %s",
			truncate(inst.Name, 39),
			inst.InstanceID,
			inst.PrivateIP,
			inst.InstanceType,
			inst.Profile.Name,
		)

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
