package cli

import (
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newThemeCommand() *cobra.Command {
	themeCmd := &cobra.Command{
		Use:     "theme",
		Example: "sshush theme show\nsshush theme list\nsshush theme set dracula",
		Long:    "Show or set the colour theme. Theme used to style the CLI and TUI.",
		Short:   "Show or set the colour theme",
	}
	themeCmd.AddCommand(newThemeShowCommand())
	themeCmd.AddCommand(newThemeListCommand())
	themeCmd.AddCommand(newThemeSetCommand())
	return themeCmd
}

func newThemeListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Example: "sshush theme list",
		Short:   "List available theme presets",
		Args:    cobra.NoArgs,
		RunE:    runThemeList,
	}
}

func runThemeList(cmd *cobra.Command, _ []string) error {
	path, _ := runtime.ResolveConfigPath(cmd.Root())
	tildePath := utils.ContractHomeDirectory(path)
	th := config.LoadThemeFromPath(path)
	currentName := presetNameForTheme(th)
	isCustom := currentName == ""

	out := style.NewOutput()
	out.Add(style.Text("Set theme in config (e.g. ") + style.Focus(tildePath) + style.Text(")"))
	out.Add(style.Text("or sshush theme set <name>"))
	out.Spacer()
	out.Add(style.Focus("Available themes:"))
	for _, name := range theme.PresetNamesOrdered() {
		if name == currentName {
			out.Add(style.Focus("* " + name))
		} else {
			out.Add(style.Text(name))
		}
	}
	if isCustom {
		out.Add(style.Focus("* custom"))
	}
	out.Print()
	return nil
}

func newThemeShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "show",
		Example: "sshush theme show",
		Short:   "Show the current theme",
		Args:    cobra.NoArgs,
		RunE:    runThemeShow,
	}
}

func runThemeShow(cmd *cobra.Command, _ []string) error {
	path, _ := runtime.ResolveConfigPath(cmd.Root())
	th := config.LoadThemeFromPath(path)
	presetName := presetNameForTheme(th)
	style.SetTheme(th)
	if presetName != "" {
		style.NewOutput().
			Add(style.Success("Theme: " + presetName)).
			Add(style.Text("  text:    ") + style.HexWithBackground(th.Text)).
			Add(style.Text("  focus:   ") + style.HexWithBackground(th.Focus)).
			Add(style.Text("  accent:  ") + style.HexWithBackground(th.Accent)).
			Add(style.Text("  error:   ") + style.HexWithBackground(th.Error)).
			Add(style.Text("  warning: ") + style.HexWithBackground(th.Warning)).
			Print()
	} else {
		style.NewOutput().
			Add(style.Highlight("Theme: custom")).
			Add(style.Text("  text:    ") + style.HexWithBackground(th.Text)).
			Add(style.Text("  focus:   ") + style.HexWithBackground(th.Focus)).
			Add(style.Text("  accent:  ") + style.HexWithBackground(th.Accent)).
			Add(style.Text("  error:   ") + style.HexWithBackground(th.Error)).
			Add(style.Text("  warning: ") + style.HexWithBackground(th.Warning)).
			Print()
	}
	return nil
}

func presetNameForTheme(th theme.Theme) string {
	for name, t := range theme.Presets {
		if themeEqual(t, th) {
			return name
		}
	}
	return ""
}

func themeEqual(a, b theme.Theme) bool {
	return a.Text == b.Text && a.Focus == b.Focus && a.Accent == b.Accent && a.Error == b.Error && a.Warning == b.Warning
}

func newThemeSetCommand() *cobra.Command {
	var text, focus, accent, errClr, warning string
	cmd := &cobra.Command{
		Use:   "set [preset]",
		Short: "Set the theme (preset name or custom hex)",
		Long:  "Set theme to a preset (e.g. dracula, nord) or use flags for custom colours. Preset and flags are mutually exclusive.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runThemeSet(cmd, args, text, focus, accent, errClr, warning)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "text colour (hex #RRGGBB)")
	cmd.Flags().StringVar(&focus, "focus", "", "focus colour (hex #RRGGBB)")
	cmd.Flags().StringVar(&accent, "accent", "", "accent colour (hex #RRGGBB)")
	cmd.Flags().StringVar(&errClr, "error", "", "error colour (hex #RRGGBB)")
	cmd.Flags().StringVar(&warning, "warning", "", "warning colour (hex #RRGGBB)")
	return cmd
}

func runThemeSet(cmd *cobra.Command, args []string, text, focus, accent, errClr, warning string) error {
	path, _ := runtime.ResolveConfigPath(cmd.Root())

	if len(args) >= 1 {
		presetName := args[0]
		if t, ok := theme.ResolveTheme(presetName); !ok {
			return style.NewOutput().Error("unknown preset: " + presetName).AsError()
		} else {
			_ = t
		}
		if err := config.WriteThemeToPath(path, presetName, nil); err != nil {
			return style.NewOutput().Error("write config: " + err.Error()).AsError()
		}
		style.NewOutput().Success("Theme set to " + presetName).Print()
		return nil
	}

	hasCustom := text != "" || focus != "" || accent != "" || errClr != "" || warning != ""
	if !hasCustom {
		return style.NewOutput().Error("provide a preset name or at least one colour flag (--text, --focus, --accent, --error, --warning)").AsError()
	}

	// Load current theme and merge
	th := config.LoadThemeFromPath(path)
	custom := theme.Theme{
		Text:    th.Text,
		Focus:   th.Focus,
		Accent:  th.Accent,
		Error:   th.Error,
		Warning: th.Warning,
	}
	if text != "" {
		if !theme.ValidHex(text) {
			return style.NewOutput().Error("invalid hex for --text: " + text).AsError()
		}
		custom.Text = text
	}
	if focus != "" {
		if !theme.ValidHex(focus) {
			return style.NewOutput().Error("invalid hex for --focus: " + focus).AsError()
		}
		custom.Focus = focus
	}
	if accent != "" {
		if !theme.ValidHex(accent) {
			return style.NewOutput().Error("invalid hex for --accent: " + accent).AsError()
		}
		custom.Accent = accent
	}
	if errClr != "" {
		if !theme.ValidHex(errClr) {
			return style.NewOutput().Error("invalid hex for --error: " + errClr).AsError()
		}
		custom.Error = errClr
	}
	if warning != "" {
		if !theme.ValidHex(warning) {
			return style.NewOutput().Error("invalid hex for --warning: " + warning).AsError()
		}
		custom.Warning = warning
	}

	section := &config.ThemeSection{
		Text:    custom.Text,
		Focus:   custom.Focus,
		Accent:  custom.Accent,
		Error:   custom.Error,
		Warning: custom.Warning,
	}
	if err := config.WriteThemeToPath(path, "", section); err != nil {
		return style.NewOutput().Error("write config: " + err.Error()).AsError()
	}
	style.NewOutput().Success("Theme set to custom colours").Print()
	return nil
}
