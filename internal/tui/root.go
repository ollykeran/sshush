package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
)

func inZoneBounds(id string, x, y int) bool {
	z := zone.Get(id)
	if z == nil {
		return false
	}
	return x >= z.StartX && x <= z.EndX && y >= z.StartY && y <= z.EndY
}

type Screen interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View(width, height int, active bool) string
	HelpEntries() []string
}

type ButtonFlashDoneMsg struct{}

func ButtonFlashCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(200 * time.Millisecond)
		return ButtonFlashDoneMsg{}
	}
}

type NavToTabBarMsg struct{}

func navToTabBarCmd() tea.Cmd {
	return func() tea.Msg {
		return NavToTabBarMsg{}
	}
}

var tabNames = []string{"sshushd", "Create", "Edit", "Export"}

type RootModel struct {
	screens      []Screen
	activeTab    int
	tabBarActive bool
	width        int
	height       int
	showHelp     bool
	quitting     bool
}

func NewRootModel(configPath, socketPath string) RootModel {
	return RootModel{
		screens: []Screen{
			NewAgentScreen(configPath, socketPath),
			NewCreateScreen(),
			NewEditScreen(socketPath),
			NewExportScreen(socketPath),
		},
	}
}

func (m RootModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.screens {
		if cmd := s.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m RootModel) switchTab(idx int) (RootModel, tea.Cmd) {
	m.activeTab = idx
	var cmd tea.Cmd
	m.screens[m.activeTab], cmd = m.screens[m.activeTab].Update(tea.WindowSizeMsg{
		Width: m.width, Height: m.height,
	})
	return m, cmd
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		var cmd tea.Cmd
		m.screens[m.activeTab], cmd = m.screens[m.activeTab].Update(msg)
		return m, cmd

	case NavToTabBarMsg:
		m.tabBarActive = true
		return m, nil

	case tea.KeyPressMsg:
		if m.showHelp {
			switch msg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "q", "esc", "?":
				m.showHelp = false
				return m, nil
			}
			return m, nil
		}

		if m.tabBarActive {
			switch msg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "left", "h":
				idx := (m.activeTab - 1 + len(m.screens)) % len(m.screens)
				m, cmd := m.switchTab(idx)
				return m, cmd
			case "right", "l":
				idx := (m.activeTab + 1) % len(m.screens)
				m, cmd := m.switchTab(idx)
				return m, cmd
			case "down", "j", "enter":
				m.tabBarActive = false
				return m, nil
			case "?":
				m.showHelp = true
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			idx := (m.activeTab + 1) % len(m.screens)
			m, cmd := m.switchTab(idx)
			return m, cmd
		case "shift+tab":
			idx := (m.activeTab - 1 + len(m.screens)) % len(m.screens)
			m, cmd := m.switchTab(idx)
			return m, cmd
		case "?":
			m.showHelp = true
			return m, nil
		}

	case tea.MouseReleaseMsg:
		if m.showHelp || msg.Button != tea.MouseLeft {
			return m, nil
		}
		for i, name := range tabNames {
			if inZoneBounds("tab-"+name, msg.X, msg.Y) {
				m.tabBarActive = false
				return m.switchTab(i)
			}
		}
		var cmd tea.Cmd
		m.screens[m.activeTab], cmd = m.screens[m.activeTab].Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.screens[m.activeTab], cmd = m.screens[m.activeTab].Update(msg)
	return m, cmd
}

func (m RootModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	w := m.width
	if w < 1 {
		w = 80
	}
	h := m.height
	if h < 1 {
		h = 24
	}

	if m.showHelp {
		entries := m.screens[m.activeTab].HelpEntries()
		common := []string{
			"",
			HelpRow("Tab", "Next screen"),
			HelpRow("Shift+Tab", "Previous screen"),
			HelpRow("?", "Toggle help"),
			HelpRow("Ctrl+c", "Quit"),
			"",
			DimStyle.Render("  Press ? or Esc to close"),
		}
		entries = append(entries, common...)
		content := HelpOverlay(entries, w, h)
		v := tea.NewView(zone.Scan(content))
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	tabBar := RenderTabBar(tabNames, m.activeTab, w, m.tabBarActive)
	contentH := h - 4
	if contentH < 3 {
		contentH = 3
	}

	screenContent := m.screens[m.activeTab].View(w, contentH, !m.tabBarActive)
	helpHint := HelpHint(w)
	content := tabBar + "\n" + screenContent + "\n" + helpHint

	v := tea.NewView(zone.Scan(content))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
