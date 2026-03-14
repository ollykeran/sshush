package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/theme"
	bl "github.com/winder/bubblelayout"
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
	pages                   []skeletonPage
	widgets                 []skeletonWidget
	activeTab               int
	navFocus                skeletonNavFocus
	navFocusBeforeDaemon    skeletonNavFocus
	activeTabBeforeDaemon   int
	width                   int
	height                  int
	layoutContentW          int
	layoutContentH          int
	layout                  bl.BubbleLayout
	contentID               bl.ID
	showHelp                bool
	quitting                bool
	KeyMap                  SkeletonKeyMap
	theme                   theme.Theme
	styles                  Styles
	configPath              string
	showThemePicker         bool
	themePickerIndex        int
	themePickerSearch       string
	themePickerScrollOffset int
	themeBeforePicker       theme.Theme // restored on Esc so we don't save
	themeMessage            string      // footer message: session only, saved, save failed
}

// #region agent log
func debugLogThemeMessage(hypothesisID, event, value string) {
	f, err := os.OpenFile("/home/ollyk/git/sshush/.cursor/debug-45ff5f.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	payload := map[string]interface{}{
		"sessionId":    "45ff5f",
		"runId":        "theme-message",
		"hypothesisId": hypothesisID,
		"location":     "internal/tui/skeleton.go",
		"message":      event,
		"data":         map[string]interface{}{"value": value},
		"timestamp":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

// #endregion

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
	navFocusDaemon
)

const (
	minTermWidth          = 120
	minTermHeight         = 30
	themePickerMaxVisible = 10
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

// themePickerFilteredOrder returns themePickerOrder filtered by themePickerSearch (case-insensitive substring).
func (s *Skeleton) themePickerFilteredOrder() []string {
	order := s.themePickerOrder()
	search := strings.TrimSpace(strings.ToLower(s.themePickerSearch))
	if search == "" {
		return order
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if strings.Contains(strings.ToLower(name), search) {
			out = append(out, name)
		}
	}
	return out
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

// themePickerClampIndexAndScroll ensures themePickerIndex is valid for filtered list and scroll offset keeps selection visible.
func (s *Skeleton) themePickerClampIndexAndScroll() {
	filtered := s.themePickerFilteredOrder()
	if len(filtered) == 0 {
		s.themePickerIndex = 0
		s.themePickerScrollOffset = 0
		return
	}
	if s.themePickerIndex >= len(filtered) {
		s.themePickerIndex = len(filtered) - 1
	}
	if s.themePickerIndex < 0 {
		s.themePickerIndex = 0
	}
	if s.themePickerIndex < s.themePickerScrollOffset {
		s.themePickerScrollOffset = s.themePickerIndex
	}
	if s.themePickerIndex >= s.themePickerScrollOffset+themePickerMaxVisible {
		s.themePickerScrollOffset = s.themePickerIndex - themePickerMaxVisible + 1
	}
	if s.themePickerScrollOffset < 0 {
		s.themePickerScrollOffset = 0
	}
}

func themeEqual(a, b theme.Theme) bool {
	return a.Text == b.Text && a.Focus == b.Focus && a.Accent == b.Accent && a.Error == b.Error && a.Warning == b.Warning
}

// NewSkeleton returns a new Skeleton with default keymap and nav focus.
func NewSkeleton() *Skeleton {
	layout := bl.New()
	contentID := layout.Add("grow")
	return &Skeleton{
		navFocus:  navFocusTabs,
		layout:    layout,
		contentID: contentID,
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
	w, h := s.layoutContentW, s.layoutContentH
	if w < 1 {
		w = s.GetTerminalWidth() - 2
	}
	if h < 1 {
		h = s.contentHeight()
		if h < 1 {
			h = 1
		}
	}
	updated, cmd := s.pages[s.activeTab].model.Update(tea.WindowSizeMsg{Width: w, Height: h})
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
	s.navFocus = navFocusTabs
	idx := s.activeTab - 1
	if idx < 0 {
		idx = len(s.pages) - 1
	}
	return s, s.switchTab(idx)
}

func (s *Skeleton) navMoveRight() (tea.Model, tea.Cmd) {
	s.navFocus = navFocusTabs
	idx := s.activeTab + 1
	if idx >= len(s.pages) {
		idx = 0
	}
	return s, s.switchTab(idx)
}

func (s *Skeleton) exitDaemonFocus() (tea.Model, tea.Cmd) {
	s.navFocus = s.navFocusBeforeDaemon
	s.activeTab = s.activeTabBeforeDaemon
	if agent := s.agentScreen(); agent != nil {
		agent.buttons.Active = 0
	}
	return s, nil
}

func (s *Skeleton) enterDaemonFocus() (tea.Model, tea.Cmd) {
	s.navFocusBeforeDaemon = s.navFocus
	s.activeTabBeforeDaemon = s.activeTab
	s.navFocus = navFocusDaemon
	if agent := s.agentScreen(); agent != nil {
		agent.buttons.Active = 0
	}
	// Switch view to agent screen when activating daemon bar
	if s.activeTab != 0 {
		return s, s.switchTab(0)
	}
	return s, nil
}

func (s *Skeleton) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if len(s.pages) == 0 {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		innerW := msg.Width - 2
		ch := s.contentHeight()
		if ch < 1 {
			ch = 1
		}
		layoutMsg := s.layout.Resize(innerW, ch)
		if sz, err := layoutMsg.Size(s.contentID); err == nil {
			s.layoutContentW = sz.Width
			s.layoutContentH = sz.Height
		} else {
			s.layoutContentW = innerW
			s.layoutContentH = ch
		}
		adjusted := tea.WindowSizeMsg{Width: s.layoutContentW, Height: s.layoutContentH}
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
			if s.navFocus == navFocusScreen || (key != "q" && key != "esc") {
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
				s.themePickerSearch = ""
				s.themePickerScrollOffset = 0
				s.themeBeforePicker = s.theme
				s.themeMessage = ""
				s.themePickerClampIndexAndScroll()
				s.navFocus = navFocusScreen
				return s, nil
			}
		}

		// Global daemon hotkeys s/x/r (when not in text input; theme picker keeps "s" for save)
		if (key == "s" || key == "x" || key == "r") && !s.showThemePicker {
			textInputActive := false
			if tip, ok := s.pages[s.activeTab].model.(interface{ HasActiveTextInput() bool }); ok {
				textInputActive = tip.HasActiveTextInput()
			}
			if !textInputActive {
				if agent := s.agentScreen(); agent != nil {
					var cmd tea.Cmd
					switch key {
					case "s":
						_, cmd = agent.pressButton(0)
					case "x":
						_, cmd = agent.pressButton(1)
					case "r":
						_, cmd = agent.pressButton(2)
					}
					return s, cmd
				}
			}
		}

		// Daemon focus: left/right, enter, s/x/r, d/q/esc exit
		if s.navFocus == navFocusDaemon {
			switch key {
			case "ctrl+c":
				s.quitting = true
				return s, tea.Quit
			case "d", "q", "esc":
				return s.exitDaemonFocus()
			case "left", "h":
				if agent := s.agentScreen(); agent != nil && agent.buttons.Active > 0 {
					agent.buttons.Left()
				}
				return s, nil
			case "right", "l":
				if agent := s.agentScreen(); agent != nil && agent.buttons.Active < len(agent.buttons.Labels)-1 {
					agent.buttons.Right()
				}
				return s, nil
			case "enter":
				if agent := s.agentScreen(); agent != nil {
					_, cmd := agent.pressButton(agent.buttons.Active)
					return s, cmd
				}
				return s, nil
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
			case "?":
				s.showHelp = true
				return s, nil
			}
			return s, nil
		}

		// 'd' key: enter daemon (when not in text field)
		if key == "d" {
			tip, hasInput := s.pages[s.activeTab].model.(interface{ HasActiveTextInput() bool })
			if !hasInput || !tip.HasActiveTextInput() {
				return s.enterDaemonFocus()
			}
		}

		if s.navFocus != navFocusScreen {
			if key == "q" || key == "esc" {
				s.quitting = true
				return s, tea.Quit
			}
			switch {
			case key == "ctrl+c":
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
				s.navFocus = navFocusScreen
				return s, nil
			case key == "?":
				s.showHelp = true
				return s, nil
			}
			return s, nil
		}

		if s.showThemePicker {
			order := s.themePickerFilteredOrder()
			switch key {
			case "esc", "escape":
				if s.themePickerSearch != "" {
					s.themePickerSearch = ""
					s.themePickerClampIndexAndScroll()
				} else {
					cmd := s.SetTheme(s.themeBeforePicker)
					s.showThemePicker = false
					return s, cmd
				}
				return s, nil
			case "q":
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
							s.themeMessage = "save failed"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						} else {
							s.themeBeforePicker = s.theme
							s.themeMessage = "saved"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						}
					} else {
						if err := config.WriteThemeToPath(s.configPath, presetName, nil); err != nil {
							s.themeMessage = "save failed"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						} else {
							s.themeBeforePicker = s.theme
							s.themeMessage = "saved"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						}
					}
				}
				s.showThemePicker = false
				return s, themeMessageTimeoutCmd()
			case "up", "k":
				if s.themePickerIndex > 0 {
					s.themePickerIndex--
					s.themePickerClampIndexAndScroll()
					order := s.themePickerFilteredOrder()
					if s.themePickerIndex < len(order) {
						presetName := order[s.themePickerIndex]
						if t, ok := s.themeForPickerChoice(presetName); ok {
							s.themeMessage = "loaded for this session"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
							return s, tea.Batch(s.SetTheme(t), themeMessageTimeoutCmd())
						}
					}
				}
				return s, nil
			case "down", "j":
				if s.themePickerIndex < len(order)-1 {
					s.themePickerIndex++
					s.themePickerClampIndexAndScroll()
					if s.themePickerIndex < len(order) {
						presetName := order[s.themePickerIndex]
						if t, ok := s.themeForPickerChoice(presetName); ok {
							s.themeMessage = "loaded for this session"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
							return s, tea.Batch(s.SetTheme(t), themeMessageTimeoutCmd())
						}
					}
				}
				return s, nil
			case "backspace":
				if len(s.themePickerSearch) > 0 {
					order := s.themePickerFilteredOrder()
					presetName := ""
					if s.themePickerIndex < len(order) {
						presetName = order[s.themePickerIndex]
					}
					s.themePickerSearch = s.themePickerSearch[:len(s.themePickerSearch)-1]
					order = s.themePickerFilteredOrder()
					if presetName != "" {
						for i, n := range order {
							if n == presetName {
								s.themePickerIndex = i
								break
							}
						}
					}
					s.themePickerClampIndexAndScroll()
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
							s.themeMessage = "save failed"
						} else {
							s.themeBeforePicker = s.theme
							s.themeMessage = "saved"
						}
					} else {
						if err := config.WriteThemeToPath(s.configPath, presetName, nil); err != nil {
							s.themeMessage = "save failed"
						} else {
							s.themeBeforePicker = s.theme
							s.themeMessage = "saved"
						}
					}
				}
				s.showThemePicker = false
				return s, themeMessageTimeoutCmd()
			default:
				// Search: single printable rune
				if len(key) == 1 && key >= " " {
					order := s.themePickerFilteredOrder()
					presetName := ""
					if s.themePickerIndex < len(order) {
						presetName = order[s.themePickerIndex]
					}
					s.themePickerSearch += key
					order = s.themePickerFilteredOrder()
					if presetName != "" {
						for i, n := range order {
							if n == presetName {
								s.themePickerIndex = i
								break
							}
						}
					}
					s.themePickerClampIndexAndScroll()
				}
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
				s.themePickerSearch = ""
				s.themePickerScrollOffset = 0
				s.themeBeforePicker = s.theme
				s.themeMessage = ""
				s.themePickerClampIndexAndScroll()
				s.navFocus = navFocusScreen
			}
			return s, nil
		}
		if s.showThemePicker {
			order := s.themePickerFilteredOrder()
			// If click is on tab or daemon, close picker (revert) and let that handle the click
			for i, p := range s.pages {
				if inZoneBounds("tab-"+p.title, msg.X, msg.Y) {
					cmd := s.SetTheme(s.themeBeforePicker)
					s.showThemePicker = false
					s.navFocus = navFocusTabs
					return s, tea.Batch(cmd, s.switchTab(i))
				}
			}
			if agent := s.agentScreen(); agent != nil {
				if btn := agent.buttons.HandleMouse(msg.X, msg.Y); btn >= 0 {
					cmd := s.SetTheme(s.themeBeforePicker)
					s.showThemePicker = false
					if s.navFocus != navFocusDaemon {
						s.navFocusBeforeDaemon = s.navFocus
						s.activeTabBeforeDaemon = s.activeTab
						s.navFocus = navFocusDaemon
					}
					agent.buttons.Active = btn
					_, pressCmd := agent.pressButton(btn)
					if s.activeTab != 0 {
						return s, tea.Batch(cmd, s.switchTab(0), pressCmd)
					}
					return s, tea.Batch(cmd, pressCmd)
				}
			}
			// Theme menu zones: visible rows are theme-picker-0 .. theme-picker-(visible-1), mapping to filtered indices start+j
			visibleCount := themePickerMaxVisible
			if len(order) < visibleCount {
				visibleCount = len(order)
			}
			start := s.themePickerScrollOffset
			if start+visibleCount > len(order) {
				start = len(order) - visibleCount
				if start < 0 {
					start = 0
				}
			}
			for j := 0; j < visibleCount; j++ {
				if inZoneBounds("theme-picker-"+strconv.Itoa(j), msg.X, msg.Y) {
					idx := start + j
					if idx < len(order) {
						s.themePickerIndex = idx
						s.themePickerClampIndexAndScroll()
						s.themeMessage = "loaded for this session"
						if t, ok := s.themeForPickerChoice(order[idx]); ok {
							return s, s.SetTheme(t)
						}
					}
					return s, nil
				}
			}
			if inZoneBounds("theme-picker-save", msg.X, msg.Y) {
				if s.themePickerIndex >= 0 && s.themePickerIndex < len(order) && s.configPath != "" {
					presetName := order[s.themePickerIndex]
					if presetName == "custom" {
						section := &config.ThemeSection{
							Text: s.theme.Text, Focus: s.theme.Focus, Accent: s.theme.Accent,
							Error: s.theme.Error, Warning: s.theme.Warning,
						}
						if err := config.WriteThemeToPath(s.configPath, "", section); err != nil {
							s.themeMessage = "save failed"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						} else {
							s.themeBeforePicker = s.theme
							s.themeMessage = "saved"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						}
					} else {
						if err := config.WriteThemeToPath(s.configPath, presetName, nil); err != nil {
							s.themeMessage = "save failed"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						} else {
							s.themeBeforePicker = s.theme
							s.themeMessage = "saved"
							debugLogThemeMessage("H1", "themeMessageSet", s.themeMessage)
						}
					}
				}
				s.showThemePicker = false
				return s, themeMessageTimeoutCmd()
			}
			// Click outside theme menu: close without saving
			cmd := s.SetTheme(s.themeBeforePicker)
			s.showThemePicker = false
			return s, cmd
		}
		// Check tab and daemon zones before modal so user can always click to navigate away
		for i, p := range s.pages {
			if inZoneBounds("tab-"+p.title, msg.X, msg.Y) {
				s.navFocus = navFocusTabs
				return s, s.switchTab(i)
			}
		}
		if agent := s.agentScreen(); agent != nil {
			if btn := agent.buttons.HandleMouse(msg.X, msg.Y); btn >= 0 {
				if s.navFocus != navFocusDaemon {
					s.navFocusBeforeDaemon = s.navFocus
					s.activeTabBeforeDaemon = s.activeTab
					s.navFocus = navFocusDaemon
				}
				agent.buttons.Active = btn
				_, cmd := agent.pressButton(btn)
				if s.activeTab != 0 {
					return s, tea.Batch(s.switchTab(0), cmd)
				}
				return s, cmd
			}
		}
		if modal, ok := s.pages[s.activeTab].model.(interface{ HasModal() bool }); ok && modal.HasModal() {
			updated, cmd := s.pages[s.activeTab].model.Update(msg)
			s.pages[s.activeTab].model = updated
			return s, cmd
		}
		// Click was not on tab or navbar; pass to page and focus screen so keys reach it
		s.navFocus = navFocusScreen
		updated, cmd := s.pages[s.activeTab].model.Update(msg)
		s.pages[s.activeTab].model = updated
		return s, cmd
	}

	switch msg := msg.(type) {
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
	case ThemeMessageClearMsg:
		if s.themeMessage != "" {
			debugLogThemeMessage("H1", "themeMessageClear", s.themeMessage)
		}
		s.themeMessage = ""
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
	// header = 3 rows, footer = 1 row
	return s.GetTerminalHeight() - 3 - 1
}

func (s *Skeleton) renderOuterHeader(w int) string {
	st := s.styles
	bc := lipgloss.NewStyle().Foreground(lipgloss.Color(st.OuterBorderColorHex))
	innerW := w - 2

	var tabParts []string
	for i, p := range s.pages {
		label := zone.Mark("tab-"+p.title, p.title)
		var style lipgloss.Style
		switch {
		case i == s.activeTab && s.navFocus == navFocusTabs:
			style = st.HeaderTabBoxActiveFocused
		case i == s.activeTab:
			style = st.HeaderTabBoxActive
		default:
			style = st.HeaderTabBoxInactive
		}
		tabParts = append(tabParts, style.Render(label))
	}

	tabsBlock := lipgloss.JoinHorizontal(lipgloss.Center, tabParts...)
	var toolsBlock string
	if agent := s.agentScreen(); agent != nil {
		btns := agent.ControlButtonsInlineView(s.navFocus == navFocusDaemon)
		inner := st.DaemonLabelStyle.Render("[d]") + " " + btns
		box := st.DaemonBoxUnfocused
		if s.navFocus == navFocusDaemon {
			box = st.DaemonBoxFocused
		}
		toolsBlock = box.Render(inner)
	}

	// Tabs left, tools right, fill between (no side padding)
	tabsW := lipgloss.Width(strings.Split(tabsBlock, "\n")[0])
	toolsW := lipgloss.Width(strings.Split(toolsBlock, "\n")[0])
	fillW := innerW - tabsW - toolsW
	if fillW < 0 {
		fillW = 0
	}
	fillLine := strings.Repeat(" ", fillW)
	fillBlock := fillLine + "\n" + fillLine + "\n" + fillLine
	headerBlock := lipgloss.JoinHorizontal(lipgloss.Top, tabsBlock, fillBlock, toolsBlock)
	lines := strings.Split(headerBlock, "\n")
	for len(lines) < 3 {
		lines = append(lines, "")
	}
	if len(lines) > 3 {
		lines = lines[:3]
	}

	var result []string
	for i, line := range lines {
		lineW := lipgloss.Width(line)
		var fill string
		if lineW >= innerW {
			line = ansi.Truncate(line, innerW, "")
			fill = ""
		} else {
			fillCh := " "
			if i == 0 {
				fillCh = "─"
			}
			fill = bc.Render(strings.Repeat(fillCh, innerW-lineW))
		}
		row := line + fill
		if lipgloss.Width(row) > innerW {
			row = ansi.Truncate(row, innerW, "")
		}
		if i == 0 {
			result = append(result, bc.Render("╭")+row+bc.Render("╮"))
		} else {
			result = append(result, bc.Render("│")+row+bc.Render("│"))
		}
	}
	return strings.Join(result, "\n")
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
		style := st.FocusStyle
		if isErr {
			style = st.ErrorStyle
		}
		leftParts = append(leftParts, style.Render(statusText))
	}

	if s.themeMessage != "" {
		leftParts = append(leftParts, st.DimStyle.Render("Theme: "+s.themeMessage))
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
			st.HelpRow("↑/k", "up"),
			st.HelpRow("↓/j", "down"),
			st.HelpRow("←/h", "left"),
			st.HelpRow("→/l", "right"),
			st.HelpRow("enter", "activate"),
			"",
			st.HelpRow("d", "Daemon controls"),
			st.HelpRow("s", "Start daemon"),
			st.HelpRow("x", "Stop daemon"),
			st.HelpRow("r", "Reload daemon"),
			"",
			st.HelpRow("t", "Theme picker"),
			"",
			st.HelpRow("?", "Toggle help"),
			st.HelpRow("esc/q/ctrl+c", "Quit"),
			"",
			st.DimStyle.Render("  Press ?/esc/q to close help"),
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
	}

	screenView := s.pages[s.activeTab].model.View()
	body := s.renderSideBorders(screenView.Content, w, contentH)

	if s.showThemePicker && len(menuLines) > 0 {
		innerW := w - 2
		border := lipgloss.NewStyle().Foreground(lipgloss.Color(s.styles.OuterBorderColorHex)).Render("│")
		menuBodyLines := make([]string, len(menuLines))
		for i, line := range menuLines {
			lineW := lipgloss.Width(line)
			pad := innerW - lineW
			if pad < 0 {
				pad = 0
			}
			menuBodyLines[i] = border + strings.Repeat(" ", pad) + line + border
		}

		bodyLines := strings.Split(body, "\n")
		maxOverlay := len(menuBodyLines)
		if maxOverlay > len(bodyLines) {
			maxOverlay = len(bodyLines)
		}
		for i := 0; i < maxOverlay; i++ {
			bodyIdx := len(bodyLines) - maxOverlay + i
			menuIdx := len(menuBodyLines) - maxOverlay + i
			bodyLines[bodyIdx] = menuBodyLines[menuIdx]
		}
		body = strings.Join(bodyLines, "\n")
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
// Uses filtered order, search row, and a fixed-height window with ellipsis when scrolled.
func (s *Skeleton) themePickerMenuBox() string {
	st := s.styles
	filtered := s.themePickerFilteredOrder()
	lines := []string{st.SectionTitleStyle.Render(" Theme")}
	// Search row (only when picker is active)
	searchLabel := "Filter: "
	if s.themePickerSearch != "" {
		searchLabel += s.themePickerSearch
	}
	lines = append(lines, st.DimStyle.Render(searchLabel))

	visibleCount := themePickerMaxVisible
	if len(filtered) < visibleCount {
		visibleCount = len(filtered)
	}
	start := s.themePickerScrollOffset
	if start+visibleCount > len(filtered) {
		start = len(filtered) - visibleCount
		if start < 0 {
			start = 0
		}
	}
	end := start + visibleCount
	if end > len(filtered) {
		end = len(filtered)
	}

	if len(filtered) > themePickerMaxVisible && start > 0 {
		lines = append(lines, st.DimStyle.Render("  ..."))
	}
	for j := start; j < end; j++ {
		name := filtered[j]
		lineContent := "  " + name
		if j == s.themePickerIndex {
			lineContent = st.FocusedButtonStyle.Render("> " + name)
		} else {
			lineContent = st.DimStyle.Render("  " + name)
		}
		lines = append(lines, zone.Mark("theme-picker-"+strconv.Itoa(j-start), lineContent))
	}
	if len(filtered) > themePickerMaxVisible && end < len(filtered) {
		lines = append(lines, st.DimStyle.Render("  ..."))
	}
	lines = append(lines, "", zone.Mark("theme-picker-save", st.DimStyle.Render("[↑↓] move [s] save [q]uit")))
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
