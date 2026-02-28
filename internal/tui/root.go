package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

var tabNames = []string{"Agent", "Create", "Edit", "Export"}

type RootModel struct {
	screens          []Screen
	activeTab        int
	tabBarActive     bool
	tabBarOnControls bool
	width            int
	height           int
	showHelp         bool
	quitting         bool
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
		m.tabBarOnControls = false
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
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			case "left", "h":
				if m.tabBarOnControls {
					agentScreen, _ := m.screens[0].(*AgentScreen)
					if agentScreen != nil && agentScreen.buttons.Active > 0 {
						agentScreen.buttons.Left()
					} else {
						m.tabBarOnControls = false
						return m.switchTab(len(m.screens) - 1)
					}
				} else if m.activeTab > 0 {
					return m.switchTab(m.activeTab - 1)
				} else {
					agentScreen, _ := m.screens[0].(*AgentScreen)
					if agentScreen != nil {
						m.tabBarOnControls = true
						agentScreen.buttons.Active = len(agentScreen.buttons.Labels) - 1
					}
				}
				return m, nil
			case "right", "l":
				if m.tabBarOnControls {
					agentScreen, _ := m.screens[0].(*AgentScreen)
					if agentScreen != nil && agentScreen.buttons.Active < len(agentScreen.buttons.Labels)-1 {
						agentScreen.buttons.Right()
					} else {
						m.tabBarOnControls = false
						return m.switchTab(0)
					}
				} else if m.activeTab < len(m.screens)-1 {
					return m.switchTab(m.activeTab + 1)
				} else {
					agentScreen, _ := m.screens[0].(*AgentScreen)
					if agentScreen != nil {
						m.tabBarOnControls = true
						agentScreen.buttons.Active = 0
					}
				}
				return m, nil
			case "down", "j":
				m.tabBarActive = false
				m.tabBarOnControls = false
				return m, nil
			case "enter":
				if m.tabBarOnControls {
					agentScreen, _ := m.screens[0].(*AgentScreen)
					if agentScreen != nil {
						_, cmd := agentScreen.pressButton(agentScreen.buttons.Active)
						return m, cmd
					}
				}
				m.tabBarActive = false
				m.tabBarOnControls = false
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
				m.tabBarOnControls = false
				return m.switchTab(i)
			}
		}
		if agentScreen, ok := m.screens[0].(*AgentScreen); ok {
			if btn := agentScreen.buttons.HandleMouse(msg.X, msg.Y); btn >= 0 {
				_, cmd := agentScreen.pressButton(btn)
				return m, cmd
			}
		}
		var cmd tea.Cmd
		m.screens[m.activeTab], cmd = m.screens[m.activeTab].Update(msg)
		return m, cmd
	}

	// Agent messages must always go to AgentScreen regardless of active tab,
	// so "stopping..." / "starting..." update to "stopped" / "started" etc.
	switch msg.(type) {
	case agentStatusMsg, agentKeysMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, foundKeysMsg, ButtonFlashDoneMsg:
		if agentScreen, ok := m.screens[0].(*AgentScreen); ok {
			var cmd tea.Cmd
			m.screens[0], cmd = agentScreen.Update(msg)
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.screens[m.activeTab], cmd = m.screens[m.activeTab].Update(msg)
	return m, cmd
}

const (
	minTermWidth  = 120
	minTermHeight = 30
)

func (m RootModel) tooSmall() bool {
	return m.width > 0 && m.height > 0 && (m.width < minTermWidth || m.height < minTermHeight)
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

	bannerStyle := BannerStyle
	agentScreen, _ := m.screens[0].(*AgentScreen)
	if agentScreen != nil {
		c := agentScreen.BannerColor()
		bannerStyle = bannerStyle.Foreground(c).BorderForeground(c)
	}
	bannerRendered := bannerStyle.Render(Banner)

	statusText := ""
	controlBtns := ""
	if agentScreen != nil {
		statusText = agentScreen.StatusText()
		controlsFocused := m.tabBarActive && m.tabBarOnControls
		controlBtns = agentScreen.ControlButtonsView(controlsFocused)
	}

	bannerW := lipgloss.Width(bannerRendered)
	bannerH := lipgloss.Height(bannerRendered)
	sizeInfo := DimStyle.Render(fmt.Sprintf("%dx%d", w, h))
	if m.tooSmall() {
		sizeInfo = WarnStyle.Render(fmt.Sprintf("⚠ Terminal too small (%dx%d), resize to %dx%d", w, h, minTermWidth, minTermHeight))
	}
	rightBlock := lipgloss.NewStyle().
		Width(w - bannerW - 2).
		Height(bannerH).
		Align(lipgloss.Right).
		Render(sizeInfo + "\n" + statusText)
	topRow := lipgloss.JoinHorizontal(lipgloss.Bottom, bannerRendered, "  ", rightBlock)

	tabsFocused := m.tabBarActive && !m.tabBarOnControls
	tabBar := RenderTabBar(tabNames, m.activeTab, w, tabsFocused, controlBtns)
	contentH := h - 12
	if contentH < 3 {
		contentH = 3
	}

	screenContent := m.screens[m.activeTab].View(w, contentH, !m.tabBarActive)
	helpHint := HelpHint(w, h)
	content := topRow + "\n" + tabBar + "\n" + screenContent + "\n" + helpHint

	v := tea.NewView(zone.Scan(content))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
