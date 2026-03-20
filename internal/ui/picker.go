package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// PickerItem is an item that can be shown in the picker.
type PickerItem struct {
	ID      string // returned on selection
	Display string // shown in the list
	Search  string // searched against (lowercased internally)
}

type pickerModel struct {
	title    string
	items    []PickerItem
	filtered []PickerItem
	cursor   int
	search   textinput.Model
	selected *PickerItem
	quitting bool
}

func newPickerModel(title string, items []PickerItem) pickerModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.Focus()

	return pickerModel{
		title:    title,
		items:    items,
		filtered: items,
		search:   ti,
	}
}

func (m pickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	query := strings.ToLower(m.search.Value())
	m.filtered = nil
	for _, item := range m.items {
		if strings.Contains(strings.ToLower(item.Search), query) {
			m.filtered = append(m.filtered, item)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}

	return m, cmd
}

func (m pickerModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString(headerStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.search.View())
	b.WriteString("\n\n")

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
		item := m.filtered[i]
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("> " + item.Display))
		} else {
			b.WriteString("  " + item.Display)
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("\n%s",
		dimStyle.Render(fmt.Sprintf("  %d/%d | ↑↓ navigate | enter select | esc quit",
			len(m.filtered), len(m.items)))))

	return b.String()
}

// Pick shows an interactive picker and returns the selected item ID.
func Pick(title string, items []PickerItem) (string, error) {
	p := tea.NewProgram(newPickerModel(title, items))
	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("UI error: %w", err)
	}

	m := result.(pickerModel)
	if m.selected == nil {
		return "", fmt.Errorf("cancelled")
	}
	return m.selected.ID, nil
}
