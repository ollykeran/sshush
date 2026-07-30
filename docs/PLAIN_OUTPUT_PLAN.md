# Defancy: Plain Output Mode — Implementation Plan

**Issue:** #33  
**Branch:** `defancy-plain-output-mode`  
**Goal:** Config option/env var/flag to send all output to stdout/stderr as plain ASCII (no colours, no fancy formatting, strip all boxes). Affects both CLI and TUI.

## Driver

`NO_COLOR` env var. If set to any non-empty value, plain mode activates.  
Also exposed as `--no-color` CLI flag and (optionally) `[theme].no_color = true` in config.

---

## Files to modify (in implementation order)

### 1. `internal/style/style.go` — Core plain mode toggle

Add:
- `var plainMode bool` (package-level, guarded by `mu`)
- `SetPlainMode(bool)` — setter, calls `rebuildStyles()` when changed
- `IsPlainMode() bool` — getter (for TUI to query)

Modify `rebuildStyles()`:
- When `plainMode`, set all lipgloss styles to `lipgloss.NewStyle()` (no colour, no bold, no nothing)

Modify `Output.Box()`:
- When plain mode, return `o.String()` directly (no box rendering)

Modify `renderBox()`:
- When plain mode, return `s` directly

Modify `HexWithBackground()`:
- When plain mode, return `hex` directly

Modify `Box()` / `BoxWithMaxWidth()`:
- When plain mode, return content without border

### 2. `internal/style/style_test.go` — Tests for plain mode

Test cases:
- `TestSetPlainMode_disables_styles` — Success/Text/Warn/Err/Dim/Highlight return plain strings without ANSI escapes
- `TestOutput_Box_returns_plain_string_when_plain` — Output.Box() returns no box in plain mode
- `TestOutput_PrintTo_plain_mode` — printed output has no ANSI/box border chars
- `TestHexWithBackground_plain` — returns hex string without ANSI in plain mode
- `TestRenderBox_returns_content_when_plain` — renderBox returns content as-is

### 3. `internal/cli/root.go` — `--no-color` flag and `NO_COLOR` env var

Add:
- `root.PersistentFlags().Bool("no-color", false, "disable colours and fancy output (also via NO_COLOR)")`
- In `PersistentPreRunE`, after `style.SetTheme(...)`, check:
  1. `NO_COLOR` env var (any non-empty value)
  2. `--no-color` flag
  If either is set, call `style.SetPlainMode(true)`

### 4. `internal/cli/root_test.go` — CLI flag/env var tests

Test cases:
- `TestNoColor_flag_activates_plain_mode` — run root command with `--no-color`, verify style.IsPlainMode()
- `TestNOCOLOR_env_activates_plain_mode` — set NO_COLOR=1, run root command, verify
- `TestNOCOLOR_unset_does_not_activate` — NO_COLOR unset, verify plain mode is false

These tests should use `cmd.SetArgs()` and `cmd.Execute()` and then check the side effect on style package.

### 5. `internal/config/config.go` (optional) — Config file option

Add `NoColor bool` to ThemeSection:
```go
type ThemeSection struct {
    ...
    NoColor bool `toml:"no_color"`
}
```

Pass through to Config struct similarly. Then in `PersistentPreRunE`, also check `cfg.Theme.NoColor`.

### 6. `internal/config/config_test.go` — Config tests

Test cases:
- `TestConfig_plain_mode` — write a config with `[theme]\nno_color = true`, load it, verify Theme.NoColor is true

### 7. `internal/tui/theme.go` — TUI plain styles

Modify:
- `BuildStyles(t)` — when plain mode is active, return empty/no-colour lipgloss styles for all 30+ fields
- The simplest approach: set all coloured styles to `lipgloss.NewStyle()` (no foreground, no background, no border)

### 8. `internal/tui/skeleton.go` — TUI plain borders

Modify rendering in plain mode:
- `renderOuterHeader()` — use `+--+` instead of `╭──╮`, `|` instead of `│`
- `renderOuterFooter()` — use `+--` and `--+` instead of `╰──╯`
- `renderSideBorders()` — use `|` instead of `│`
- `themePickerMenuBox()` and `helpOverlay()` — use ASCII box drawing (`+`, `-`, `|`)
- `View()` — no special alt screen or mouse mode in plain mode

Check `style.IsPlainMode()` in `NewTUI()` and in `View()` to switch rendering.

### 9. `docs/config.md` — Documentation

Document `[theme].no_color` option:

---

## Verification

Run `go test ./... -race -count=1` to verify all tests pass.
