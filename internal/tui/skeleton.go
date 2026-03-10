package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/theme"
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
	pages             []skeletonPage
	widgets           []skeletonWidget
	activeTab         int
	navFocus          skeletonNavFocus
	width             int
	height            int
	showHelp          bool
	quitting          bool
	KeyMap            SkeletonKeyMap
	theme             theme.Theme
	styles            Styles
	configPath        string
	showThemePicker   bool
	themePickerIndex  int
	themeBeforePicker theme.Theme // restored on Esc so we don't save
}

// Styles returns the current styles (derived from theme). Use for all TUI rendering.
func (s *Skeleton) Styles() Styles { return s.styles }

// Theme returns the current theme. Use for color conversion (e.g. BannerColor).
func (s *Skeleton) Theme() theme.Theme { return s.theme }

// SetTheme updates the theme and rebuilds styles. Call after config write in theme picker.
// Returns a Cmd that sends ThemeChangedMsg so screens can refresh KeyTable etc.
func (s *Skeleton) SetTheme(t theme.Theme) tea.Cmd {
	s.theme = t
	s.styles = BuildStyles(t)
	return themeChangedCmd()
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

var themePresetOrder = theme.PresetNamesOrdered()

func (s *Skeleton) currentThemePresetIndex() int {
	for i, name := range themePresetOrder {
		if t, ok := theme.ResolveTheme(name); ok && themeEqual(t, s.theme) {
			return i
		}
	}
	return 0
}

// themePickerOrder returns preset names for the picker; appends "custom" if current theme matches no preset.
func (s *Skeleton) themePickerOrder() []string {
	order := make([]string, 0, len(themePresetOrder)+1)
	order = append(order, themePresetOrder...)
	matched := false
	for _, name := range themePresetOrder {
		if t, ok := theme.ResolveTheme(name); ok && themeEqual(t, s.theme) {
			matched = true
			break
		}
	}
	if !matched {
		order = append(order, "custom")
	}
	return order
}

func (s *Skeleton) currentThemePickerIndex() int {
	order := s.themePickerOrder()
	for i, name := range order {
		if name == "custom" {
			matched := false
			for _, n := range themePresetOrder {
				if t, ok := theme.ResolveTheme(n); ok && themeEqual(t, s.theme) {
					matched = true
					break
				}
			}
			if !matched {
				return i
			}
			continue
		}
		if t, ok := theme.ResolveTheme(name); ok && themeEqual(t, s.theme) {
			return i
		}
	}
	return 0
}

// themeForPickerChoice returns the theme for a picker choice (preset name or "custom").
func (s *Skeleton) themeForPickerChoice(name string) (theme.Theme, bool) {
	if name == "custom" {
		return s.theme, true
	}
	return theme.ResolveTheme(name)
}

func themeEqual(a, b theme.Theme) bool {
	return a.Text == b.Text && a.Focus == b.Focus && a.Accent == b.Accent && a.Error == b.Error && a.Warning == b.Warning
}

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

		if s.navFocus != navFocusScreen {
			if key == "q" || key == "esc" {
				s.quitting = true
				return s, tea.Quit
			}
		}

		if modal, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok && modal.HasModal() {
			if s.navFocus == navFocusScreen || key == "q" || key == "esc" {
				updated, cmd := s.pages[s.activeTab].model.Update(msg)
				s.pages[s.activeTab].model = updated
				return s, cmd
			}
		}

		// "t" opens theme picker from anywhere (screen or navbar), but not when file picker or text input is active
		if key == "t" {
			modalActive := false
			if m, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok {
				modalActive = m.HasModal()
			}
			textInputActive := false
			if tip, ok := s.pages[s.activeTab].model.(interface{ HasActiveTextInput() bool }); ok {
				textInputActive = tip.HasActiveTextInput()
			}
			if !modalActive && !textInputActive {
				s.showThemePicker = true
				s.themePickerIndex = s.currentThemePickerIndex()
				s.themeBeforePicker = s.theme
				s.navFocus = navFocusScreen
				return s, nil
			}
		}

		if s.navFocus != navFocusScreen {
			// Daemon shortcuts work when focus is in navbar (tabs/tools)
			switch key {
			case "s":
				if agent := s.agentScreen(); agent != nil {
					_, cmd := agent.pressButton(0)
					return s, cmd
				}
				return s, nil
			case "x":
				if agent := s.agentScreen(); agent != nil {
					_, cmd := agent.pressButton(1)
					return s, cmd
				}
				return s, nil
			case "r":
				if agent := s.agentScreen(); agent != nil {
					_, cmd := agent.pressButton(2)
					return s, cmd
				}
				return s, nil
			}
			switch {
			case key == "ctrl+c" || key == "q" || key == "esc":
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

		if s.showThemePicker {
			order := s.themePickerOrder()
			switch key {
			case "esc", "escape", "q":
				cmd := s.SetTheme(s.themeBeforePicker)
				s.showThemePicker = false
				return s, cmd
			case "enter":
				// Save and close (same as "s")
				if s.themePickerIndex >= 0 && s.themePickerIndex < len(order) && s.configPath != "" {
					presetName := order[s.themePickerIndex]
					if presetName == "custom" {
						section := &config.ThemeSection{
							Text: s.theme.Text, Focus: s.theme.Focus, Accent: s.theme.Accent,
							Error: s.theme.Error, Warning: s.theme.Warning,
						}
						if err := config.WriteThemeToPath(s.configPath, "", section); err != nil {
							s.UpdateWidgetValue("sshushd", "theme write failed")
						} else {
							s.themeBeforePicker = s.theme
						}
					} else {
						if err := config.WriteThemeToPath(s.configPath, presetName, nil); err != nil {
							s.UpdateWidgetValue("sshushd", "theme write failed")
						} else {
							s.themeBeforePicker = s.theme
						}
					}
				}
				s.showThemePicker = false
				return s, nil
			case "up", "k":
				if s.themePickerIndex > 0 {
					s.themePickerIndex--
					presetName := order[s.themePickerIndex]
					if t, ok := s.themeForPickerChoice(presetName); ok {
						return s, s.SetTheme(t)
					}
				}
				return s, nil
			case "down", "j":
				if s.themePickerIndex < len(order)-1 {
					s.themePickerIndex++
					presetName := order[s.themePickerIndex]
					if t, ok := s.themeForPickerChoice(presetName); ok {
						return s, s.SetTheme(t)
					}
				}
				return s, nil
			case "s":
				if s.themePickerIndex >= 0 && s.themePickerIndex < len(order) && s.configPath != "" {
					presetName := order[s.themePickerIndex]
					if presetName == "custom" {
						section := &config.ThemeSection{
							Text: s.theme.Text, Focus: s.theme.Focus, Accent: s.theme.Accent,
							Error: s.theme.Error, Warning: s.theme.Warning,
						}
						if err := config.WriteThemeToPath(s.configPath, "", section); err != nil {
							s.UpdateWidgetValue("sshushd", "theme write failed")
						} else {
							s.themeBeforePicker = s.theme
						}
					} else {
						if err := config.WriteThemeToPath(s.configPath, presetName, nil); err != nil {
							s.UpdateWidgetValue("sshushd", "theme write failed")
						} else {
							s.themeBeforePicker = s.theme
						}
					}
				}
				s.showThemePicker = false
				return s, nil
			}
			return s, nil
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
		if inZoneBounds("footer-help", msg.X, msg.Y) {
			s.showHelp = true
			return s, nil
		}
		if inZoneBounds("footer-theme", msg.X, msg.Y) {
			modalActive := false
			if m, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok {
				modalActive = m.HasModal()
			}
			if !modalActive {
				s.showThemePicker = true
				s.themePickerIndex = s.currentThemePickerIndex()
				s.themeBeforePicker = s.theme
				s.navFocus = navFocusScreen
			}
			return s, nil
		}
		if s.showThemePicker {
			order := s.themePickerOrder()
			for i := 0; i < len(order); i++ {
				if inZoneBounds("theme-picker-"+strconv.Itoa(i), msg.X, msg.Y) {
					s.themePickerIndex = i
					if t, ok := s.themeForPickerChoice(order[i]); ok {
						return s, s.SetTheme(t)
					}
					return s, nil
				}
			}
		}
		if modal, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok && modal.HasModal() {
			updated, cmd := s.pages[s.activeTab].model.Update(msg)
			s.pages[s.activeTab].model = updated
			return s, cmd
		}
		for i, p := range s.pages {
			if inZoneBounds("tab-"+p.title, msg.X, msg.Y) {
				s.navFocus = navFocusTabs
				return s, s.switchTab(i)
			}
		}
		if agent := s.agentScreen(); agent != nil {
			if btn := agent.buttons.HandleMouse(msg.X, msg.Y); btn >= 0 {
				_, cmd := agent.pressButton(btn)
				return s, cmd
			}
		}
		// Click was not on tab or navbar; pass to page (e.g. table row) and focus screen so keys reach it
		s.navFocus = navFocusScreen
		updated, cmd := s.pages[s.activeTab].model.Update(msg)
		s.pages[s.activeTab].model = updated
		return s, cmd
	}

	switch msg.(type) {
	case agentStatusMsg, agentKeysMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, foundKeysMsg, ButtonFlashDoneMsg:
		updated, cmd := s.pages[0].model.Update(msg)
		s.pages[0].model = updated
		return s, cmd
	case ThemeChangedMsg:
		for i := range s.pages {
			updated, _ := s.pages[i].model.Update(msg)
			s.pages[i].model = updated
		}
		return s, nil
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
	st := s.styles
	bc := lipgloss.NewStyle().Foreground(lipgloss.Color(st.OuterBorderColorHex))

	var tabParts []string
	for i, p := range s.pages {
		label := zone.Mark("tab-"+p.title, p.title)
		var style lipgloss.Style
		switch {
		case i == s.activeTab && s.navFocus == navFocusTabs:
			style = st.HeaderTabActiveFocused
		case i == s.activeTab:
			style = st.HeaderTabActive
		default:
			style = st.HeaderTabInactive
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
	fillW := max(innerW-tabsW-toolsW, 2)

	var middle string
	if toolsW > 0 {
		before := max(fillW-1, 1)
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
	st := s.styles
	bc := lipgloss.NewStyle().Foreground(lipgloss.Color(st.OuterBorderColorHex))

	var leftParts []string

	for _, wd := range s.widgets {
		if wd.value != "" {
			leftParts = append(leftParts, st.DimStyle.Render(fmt.Sprintf("%s: %s", wd.id, wd.value)))
		}
	}

	statusText, isErr := s.statusLine()
	if statusText != "" {
		style := st.GreenStyle
		if isErr {
			style = st.ErrorStyle
		}
		leftParts = append(leftParts, style.Render(statusText))
	}

	leftContent := ""
	if len(leftParts) > 0 {
		leftContent = " " + strings.Join(leftParts, "  |  ") + " "
	}

	rightContent := " " + st.DimStyle.Render("[?] help") + " "

	sizeInfo := st.DimStyle.Render(fmt.Sprintf(" %dx%d ", w, s.GetTerminalHeight()))
	if w < minTermWidth || s.GetTerminalHeight() < minTermHeight {
		sizeInfo = st.WarnStyle.Render(fmt.Sprintf(" %dx%d ", w, s.GetTerminalHeight()))
	}

	themeWidget := " " + st.DimStyle.Render("[t] theme") + " "
	themeWidgetMarked := zone.Mark("footer-theme", themeWidget)
	helpWidgetMarked := zone.Mark("footer-help", rightContent)

	leftW := lipgloss.Width(leftContent)
	rightContentW := lipgloss.Width(rightContent)
	themeWidgetW := lipgloss.Width(themeWidget)
	sizeInfoW := lipgloss.Width(sizeInfo)

	// ╰─(2) + leftContent + fill + sizeInfo + ─(1) + theme + rightContent + ─╯(2) = w
	fillW := w - leftW - rightContentW - themeWidgetW - sizeInfoW - 6
	if fillW < 1 {
		fillW = 1
	}

	return bc.Render("╰─") +
		leftContent +
		bc.Render(strings.Repeat("─", fillW)) +
		sizeInfo +
		bc.Render("─") +
		themeWidgetMarked +
		bc.Render("─") +
		helpWidgetMarked +
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
		st := s.styles
		common := []string{
			"",
			st.HelpRow("Tab", "Next screen"),
			st.HelpRow("Shift+Tab", "Previous screen"),
			st.HelpRow("?", "Toggle help"),
			st.HelpRow("Ctrl+c", "Quit"),
			"",
			st.DimStyle.Render("  Press ? or Esc to close"),
		}
		content := s.helpOverlay(append(entries, common...), w, h)
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
	var menuLines []string
	if s.showThemePicker {
		menuBox := s.themePickerMenuBox()
		menuLines = strings.Split(menuBox, "\n")
		contentH -= len(menuLines)
		if contentH < 1 {
			contentH = 1
		}
	}

	screenView := s.pages[s.activeTab].model.View()
	body := s.renderSideBorders(screenView.Content, w, contentH)

	if s.showThemePicker && len(menuLines) > 0 {
		innerW := w - 2
		bc := lipgloss.NewStyle().Foreground(lipgloss.Color(s.styles.OuterBorderColorHex)).Render("│")
		for _, line := range menuLines {
			lineW := lipgloss.Width(line)
			pad := innerW - lineW
			if pad < 0 {
				pad = 0
			}
			body += "\n" + bc + strings.Repeat(" ", pad) + line + bc
		}
	}
	content := header + "\n" + body + "\n" + footer

	v := tea.NewView(zone.Scan(content))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (s *Skeleton) renderSideBorders(content string, w, h int) string {
	bc := lipgloss.NewStyle().Foreground(lipgloss.Color(s.styles.OuterBorderColorHex))
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

// themePickerMenuBox returns a vertical menu (one theme per line) for bottom-right placement above [t]heme.
func (s *Skeleton) themePickerMenuBox() string {
	st := s.styles
	order := s.themePickerOrder()
	lines := []string{st.SectionTitleStyle.Render(" Theme")}
	for i, name := range order {
		lineContent := "  " + name
		if i == s.themePickerIndex {
			lineContent = st.FocusedButtonStyle.Render("> " + name)
		} else {
			lineContent = st.DimStyle.Render("  " + name)
		}
		lines = append(lines, zone.Mark("theme-picker-"+strconv.Itoa(i), lineContent))
	}
	lines = append(lines, "", st.DimStyle.Render("[↑↓] move [s] save [q]uit"))
	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(st.OuterBorderColorHex)).
		Padding(0, 1).
		Render(body)
}

func (s *Skeleton) themePickerView(width, height int) string {
	st := s.styles
	order := s.themePickerOrder()
	lines := []string{st.SectionTitleStyle.Render(" Theme"), ""}
	for i, name := range order {
		suffix := ""
		if i == s.currentThemePickerIndex() {
			suffix = " (current)"
		}
		if i == s.themePickerIndex {
			lines = append(lines, st.FocusedButtonStyle.Render("> "+name)+suffix)
		} else {
			lines = append(lines, st.DimStyle.Render("  "+name)+suffix)
		}
	}
	lines = append(lines, "", st.DimStyle.Render("  [s] save  Esc: close"))
	body := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(st.OuterBorderColorHex)).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (s *Skeleton) helpOverlay(lines []string, width, height int) string {
	st := s.styles
	body := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(st.OuterBorderColorHex)).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
