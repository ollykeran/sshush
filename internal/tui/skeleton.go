package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

type skeletonPage struct {
	id    string
	title string
	model tea.Model
}

type skeletonWidget struct {
	id    string
	value string
}

// SkeletonKeyMap holds key bindings for switching between tabs.
type SkeletonKeyMap struct {
	SwitchTabLeft  []string
	SwitchTabRight []string
}

// Skeleton is the main TUI layout: header with tabs and control buttons, content area, and footer.
type Skeleton struct {
	pages     []skeletonPage
	widgets   []skeletonWidget
	activeTab int
	navFocus  skeletonNavFocus
	width     int
	height    int
	showHelp  bool
	quitting  bool
	KeyMap    SkeletonKeyMap
}

type skeletonNavFocus int

const (
	navFocusScreen skeletonNavFocus = iota
	navFocusTabs
	navFocusTools
)

const (
	minTermWidth  = 120
	minTermHeight = 30
)

// NewSkeleton returns a new Skeleton with default keymap and nav focus.
func NewSkeleton() *Skeleton {
	return &Skeleton{
		navFocus: navFocusTools,
		KeyMap: SkeletonKeyMap{
			SwitchTabLeft:  []string{"ctrl+left"},
			SwitchTabRight: []string{"ctrl+right"},
		},
	}
}

func (s *Skeleton) ScreenActive() bool {
	return s.navFocus == navFocusScreen
}

func (s *Skeleton) AddPage(id, title string, model tea.Model) {
	s.pages = append(s.pages, skeletonPage{id: id, title: title, model: model})
}

func (s *Skeleton) AddWidget(id, value string) {
	s.widgets = append(s.widgets, skeletonWidget{id: id, value: value})
}

func (s *Skeleton) UpdateWidgetValue(id, value string) {
	for i := range s.widgets {
		if s.widgets[i].id == id {
			s.widgets[i].value = value
			return
		}
	}
	s.AddWidget(id, value)
}

func (s *Skeleton) GetTerminalWidth() int {
	if s.width < 1 {
		return 80
	}
	return s.width
}

func (s *Skeleton) GetTerminalHeight() int {
	if s.height < 1 {
		return 24
	}
	return s.height
}

func (s *Skeleton) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range s.pages {
		if cmd := p.model.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func (s *Skeleton) switchTab(idx int) tea.Cmd {
	if len(s.pages) == 0 {
		return nil
	}
	s.activeTab = idx
	ch := s.contentHeight()
	if ch < 1 {
		ch = 1
	}
	updated, cmd := s.pages[s.activeTab].model.Update(tea.WindowSizeMsg{Width: s.GetTerminalWidth() - 2, Height: ch})
	s.pages[s.activeTab].model = updated
	return cmd
}

func (s *Skeleton) agentScreen() *AgentScreen {
	if len(s.pages) == 0 {
		return nil
	}
	agent, _ := s.pages[0].model.(*AgentScreen)
	return agent
}

func (s *Skeleton) navMoveLeft() (tea.Model, tea.Cmd) {
	switch s.navFocus {
	case navFocusTools:
		agent := s.agentScreen()
		if agent != nil && agent.buttons.Active > 0 {
			agent.buttons.Left()
			return s, nil
		}
		s.navFocus = navFocusTabs
		return s, s.switchTab(len(s.pages) - 1)
	default:
		s.navFocus = navFocusTabs
		if s.activeTab > 0 {
			return s, s.switchTab(s.activeTab - 1)
		}
		agent := s.agentScreen()
		if agent != nil {
			s.navFocus = navFocusTools
			agent.buttons.Active = len(agent.buttons.Labels) - 1
		}
		return s, nil
	}
}

func (s *Skeleton) navMoveRight() (tea.Model, tea.Cmd) {
	switch s.navFocus {
	case navFocusTools:
		agent := s.agentScreen()
		if agent != nil && agent.buttons.Active < len(agent.buttons.Labels)-1 {
			agent.buttons.Right()
			return s, nil
		}
		s.navFocus = navFocusTabs
		return s, s.switchTab(0)
	default:
		s.navFocus = navFocusTabs
		if s.activeTab < len(s.pages)-1 {
			return s, s.switchTab(s.activeTab + 1)
		}
		agent := s.agentScreen()
		if agent != nil {
			s.navFocus = navFocusTools
			agent.buttons.Active = 0
		}
		return s, nil
	}
}

func (s *Skeleton) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if len(s.pages) == 0 {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		ch := s.contentHeight()
		if ch < 1 {
			ch = 1
		}
		adjusted := tea.WindowSizeMsg{Width: msg.Width - 2, Height: ch}
		updated, cmd := s.pages[s.activeTab].model.Update(adjusted)
		s.pages[s.activeTab].model = updated
		return s, cmd

	case NavToTabBarMsg:
		s.navFocus = navFocusTabs
		return s, nil

	case tea.KeyPressMsg:
		key := msg.String()
		if s.showHelp {
			switch key {
			case "ctrl+c":
				s.quitting = true
				return s, tea.Quit
			case "q", "esc", "?":
				s.showHelp = false
			}
			return s, nil
		}

		if modal, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok && modal.HasModal() {
			updated, cmd := s.pages[s.activeTab].model.Update(msg)
			s.pages[s.activeTab].model = updated
			return s, cmd
		}

		if s.navFocus != navFocusScreen {
			switch {
			case key == "ctrl+c" || key == "q":
				s.quitting = true
				return s, tea.Quit
			case containsKey(s.KeyMap.SwitchTabLeft, key):
				return s.navMoveLeft()
			case containsKey(s.KeyMap.SwitchTabRight, key):
				return s.navMoveRight()
			case key == "down" || key == "j":
				s.navFocus = navFocusScreen
				return s, nil
			case key == "up" || key == "k":
				s.navFocus = navFocusTabs
				return s, nil
			case key == "enter":
				if s.navFocus == navFocusTools {
					if agent := s.agentScreen(); agent != nil {
						_, cmd := agent.pressButton(agent.buttons.Active)
						return s, cmd
					}
				}
				s.navFocus = navFocusScreen
				return s, nil
			case key == "?":
				s.showHelp = true
				return s, nil
			}
			if s.navFocus != navFocusScreen {
				return s, nil
			}
		}

		if tip, ok := s.pages[s.activeTab].model.(interface{ HasActiveTextInput() bool }); ok && tip.HasActiveTextInput() {
			switch key {
			case "ctrl+c":
				s.quitting = true
				return s, tea.Quit
			case "esc":
				s.quitting = true
				return s, tea.Quit
			default:
				updated, cmd := s.pages[s.activeTab].model.Update(msg)
				s.pages[s.activeTab].model = updated
				return s, cmd
			}
		}

		switch {
		case key == "ctrl+c":
			s.quitting = true
			return s, tea.Quit
		case key == "s":
			if agent := s.agentScreen(); agent != nil {
				_, cmd := agent.pressButton(0)
				return s, cmd
			}
			return s, nil
		case key == "x":
			if agent := s.agentScreen(); agent != nil {
				_, cmd := agent.pressButton(1)
				return s, cmd
			}
			return s, nil
		case key == "r":
			if agent := s.agentScreen(); agent != nil {
				_, cmd := agent.pressButton(2)
				return s, cmd
			}
			return s, nil
		case key == "tab":
			idx := (s.activeTab + 1) % len(s.pages)
			return s, s.switchTab(idx)
		case key == "shift+tab":
			idx := (s.activeTab - 1 + len(s.pages)) % len(s.pages)
			return s, s.switchTab(idx)
		case key == "?":
			s.showHelp = true
			return s, nil
		}
	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.showHelp {
			return s, nil
		}
		if modal, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok && modal.HasModal() {
			updated, cmd := s.pages[s.activeTab].model.Update(msg)
			s.pages[s.activeTab].model = updated
			return s, cmd
		}
		for i, p := range s.pages {
			if inZoneBounds("tab-"+p.title, msg.X, msg.Y) {
				s.navFocus = navFocusScreen
				return s, s.switchTab(i)
			}
		}
		if agent := s.agentScreen(); agent != nil {
			if btn := agent.buttons.HandleMouse(msg.X, msg.Y); btn >= 0 {
				_, cmd := agent.pressButton(btn)
				return s, cmd
			}
		}
	}

	switch msg.(type) {
	case agentStatusMsg, agentKeysMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, foundKeysMsg, ButtonFlashDoneMsg:
		updated, cmd := s.pages[0].model.Update(msg)
		s.pages[0].model = updated
		return s, cmd
	}

	updated, cmd := s.pages[s.activeTab].model.Update(msg)
	s.pages[s.activeTab].model = updated
	return s, cmd
}

func (s *Skeleton) statusLine() (string, bool) {
	type statusProvider interface {
		StatusTextRaw() (string, bool)
	}
	if sp, ok := s.pages[s.activeTab].model.(statusProvider); ok {
		return sp.StatusTextRaw()
	}
	return "", false
}

func (s *Skeleton) contentHeight() int {
	// header = 3 rows (tab boxes), footer = 1 row
	return s.GetTerminalHeight() - 3 - 1
}

func (s *Skeleton) renderOuterHeader(w int) string {
	bc := lipgloss.NewStyle().Foreground(OuterBorderColor)

	var tabParts []string
	for i, p := range s.pages {
		label := zone.Mark("tab-"+p.title, p.title)
		var style lipgloss.Style
		switch {
		case i == s.activeTab && s.navFocus == navFocusTabs:
			style = HeaderTabActiveFocused
		case i == s.activeTab:
			style = HeaderTabActive
		default:
			style = HeaderTabInactive
		}
		tabParts = append(tabParts, style.Render(label))
	}

	tabsBlock := lipgloss.JoinHorizontal(lipgloss.Center, tabParts...)
	tabsW := lipgloss.Width(tabsBlock)

	tools := ""
	toolsW := 0
	if agent := s.agentScreen(); agent != nil {
		tools = agent.ControlButtonsView(s.navFocus == navFocusTools)
		toolsW = lipgloss.Width(tools)
	}

	innerW := w - 2
	fillW := innerW - tabsW - toolsW
	if fillW < 2 {
		fillW = 2
	}

	var middle string
	if toolsW > 0 {
		before := fillW - 1
		if before < 1 {
			before = 1
		}
		after := fillW - before
		if after < 0 {
			after = 0
		}
		middle = lipgloss.JoinHorizontal(lipgloss.Center,
			tabsBlock,
			bc.Render(strings.Repeat("─", before)),
			tools,
			bc.Render(strings.Repeat("─", after)),
		)
	} else {
		middle = lipgloss.JoinHorizontal(lipgloss.Center,
			tabsBlock,
			bc.Render(strings.Repeat("─", fillW)),
		)
	}

	leftCorner := bc.Render("╭") + "\n" + bc.Render("│")
	rightCorner := bc.Render("╮") + "\n" + bc.Render("│")

	return lipgloss.JoinHorizontal(lipgloss.Bottom, leftCorner, middle, rightCorner)
}

func (s *Skeleton) renderOuterFooter(w int) string {
	bc := lipgloss.NewStyle().Foreground(OuterBorderColor)

	var leftParts []string

	for _, wd := range s.widgets {
		if wd.value != "" {
			leftParts = append(leftParts, fmt.Sprintf("%s: %s", wd.id, wd.value))
		}
	}

	statusText, isErr := s.statusLine()
	if statusText != "" {
		st := GreenStyle
		if isErr {
			st = ErrorStyle
		}
		leftParts = append(leftParts, st.Render(statusText))
	}

	leftContent := ""
	if len(leftParts) > 0 {
		leftContent = " " + DimStyle.Render(strings.Join(leftParts, "  |  ")) + " "
	}

	rightContent := " " + DimStyle.Render("? help") + " "

	sizeInfo := DimStyle.Render(fmt.Sprintf(" %dx%d ", w, s.GetTerminalHeight()))
	if w < minTermWidth || s.GetTerminalHeight() < minTermHeight {
		sizeInfo = WarnStyle.Render(fmt.Sprintf(" %dx%d ", w, s.GetTerminalHeight()))
	}

	leftW := lipgloss.Width(leftContent)
	rightContentW := lipgloss.Width(rightContent)
	sizeInfoW := lipgloss.Width(sizeInfo)

	// ╰─(2) + leftContent + fill + sizeInfo + ─(1) + rightContent + ─╯(2) = w
	fillW := w - leftW - rightContentW - sizeInfoW - 5
	if fillW < 1 {
		fillW = 1
	}

	return bc.Render("╰─") +
		leftContent +
		bc.Render(strings.Repeat("─", fillW)) +
		sizeInfo +
		bc.Render("─") +
		rightContent +
		bc.Render("─╯")
}

func (s *Skeleton) View() tea.View {
	if s.quitting {
		return tea.NewView("")
	}

	w := s.GetTerminalWidth()
	h := s.GetTerminalHeight()

	var entries []string
	if hp, ok := s.pages[s.activeTab].model.(interface{ HelpEntries() []string }); ok {
		entries = hp.HelpEntries()
	}
	if s.showHelp {
		common := []string{
			"",
			HelpRow("Tab", "Next screen"),
			HelpRow("Shift+Tab", "Previous screen"),
			HelpRow("?", "Toggle help"),
			HelpRow("Ctrl+c", "Quit"),
			"",
			DimStyle.Render("  Press ? or Esc to close"),
		}
		content := helpOverlay(append(entries, common...), w, h)
		v := tea.NewView(content)
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	header := s.renderOuterHeader(w)
	footer := s.renderOuterFooter(w)

	contentH := s.contentHeight()
	if contentH < 1 {
		contentH = 1
	}

	screenView := s.pages[s.activeTab].model.View()
	body := renderSideBorders(screenView.Content, w, contentH)

	content := header + "\n" + body + "\n" + footer

	v := tea.NewView(zone.Scan(content))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func renderSideBorders(content string, w, h int) string {
	bc := lipgloss.NewStyle().Foreground(OuterBorderColor)
	border := bc.Render("│")
	innerW := w - 2

	lines := strings.Split(content, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}

	result := make([]string, len(lines))
	for i, line := range lines {
		lineW := lipgloss.Width(line)
		if lineW > innerW {
			line = ansi.Truncate(line, innerW, "")
		} else if lineW < innerW {
			line = line + strings.Repeat(" ", innerW-lineW)
		}
		result[i] = border + line + border
	}
	return strings.Join(result, "\n")
}

func helpOverlay(lines []string, width, height int) string {
	body := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorPurple).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
