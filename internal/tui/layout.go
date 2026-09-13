package tui

// Layout helpers shared by Skeleton and screens.
// Prefer these over copying 3/4-width clamps.

const (
	LayoutHeaderRows     = 3
	LayoutFooterRows     = 1
	LayoutSideBorderCols = 2

	SectionBoxMaxWidth = 120
	SectionBoxMinWidth = 60

	MinCreatePanelWidth         = 40
	MinWidthForHorizontalLayout = 100

	layoutSectionGap = 1
)

// ContentWidth returns the inner content width for a terminal of termW
// after side borders (same formula Skeleton uses for page sizing).
func ContentWidth(termW int) int {
	w := termW - LayoutSideBorderCols
	if w < 1 {
		return 1
	}
	return w
}

// SectionWidth returns the standard section box width: 3/4 of contentW,
// clamped between SectionBoxMinWidth and SectionBoxMaxWidth.
func SectionWidth(contentW int) int {
	w := contentW * 3 / 4
	if w > SectionBoxMaxWidth {
		w = SectionBoxMaxWidth
	}
	if w < SectionBoxMinWidth {
		w = SectionBoxMinWidth
	}
	if w > contentW && contentW > 0 {
		w = contentW
	}
	return w
}

// SectionInnerWidth is the width available inside a SectionBox border (width-4).
func SectionInnerWidth(sectionW int) int {
	inner := sectionW - 4
	if inner < 10 {
		return 10
	}
	return inner
}

// SectionGap returns the vertical gap between stacked sections.
func SectionGap() int { return layoutSectionGap }
