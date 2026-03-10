package theme

// Theme holds five semantic colour roles as hex strings (#RRGGBB).
// Used by CLI (internal/style) and TUI for consistent styling.
type Theme struct {
	Text    string
	Focus   string
	Accent  string
	Error   string
	Warning string
}
