package tui

import tea "charm.land/bubbletea/v2"

// Focusable is an optional richer focus target (button/input wrappers).
// Screens may use index-based FocusRing without implementing this.
type Focusable interface {
	Focus() tea.Cmd
	Blur()
	Update(msg tea.Msg) (Focusable, tea.Cmd)
	View() string
}

// FocusRing tracks a focused index among n slots, with optional skip.
type FocusRing struct {
	n    int
	idx  int
	skip func(int) bool
}

// NewFocusRing creates a ring with n focus slots (indices 0..n-1).
func NewFocusRing(n int) FocusRing {
	if n < 0 {
		n = 0
	}
	return FocusRing{n: n, idx: 0}
}

// SetSkip sets a predicate for indices that should be skipped when moving.
func (r *FocusRing) SetSkip(fn func(int) bool) {
	r.skip = fn
	r.clampToValid()
}

// SetIndex sets the focused index (clamped / skip-adjusted).
func (r *FocusRing) SetIndex(i int) {
	if r.n == 0 {
		r.idx = 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= r.n {
		i = r.n - 1
	}
	r.idx = i
	r.clampToValid()
}

// Index returns the focused index.
func (r FocusRing) Index() int { return r.idx }

// Len returns the number of slots.
func (r FocusRing) Len() int { return r.n }

func (r *FocusRing) clampToValid() {
	if r.n == 0 {
		r.idx = 0
		return
	}
	for tried := 0; tried < r.n; tried++ {
		if r.skip == nil || !r.skip(r.idx) {
			return
		}
		r.idx = (r.idx + 1) % r.n
	}
}

func (r *FocusRing) isSkippable(i int) bool {
	return r.skip != nil && r.skip(i)
}

// Next moves focus forward, wrapping, skipping skipped indices.
func (r *FocusRing) Next() {
	if r.n == 0 {
		return
	}
	for tried := 0; tried < r.n; tried++ {
		r.idx = (r.idx + 1) % r.n
		if !r.isSkippable(r.idx) {
			return
		}
	}
}

// Prev moves focus backward. Returns true if focus was already on the first
// non-skipped slot (caller should typically return to the tab bar).
func (r *FocusRing) Prev() bool {
	if r.n == 0 {
		return true
	}
	start := r.idx
	for tried := 0; tried < r.n; tried++ {
		next := r.idx - 1
		if next < 0 {
			return true
		}
		r.idx = next
		if !r.isSkippable(r.idx) {
			return false
		}
	}
	r.idx = start
	return true
}

// NextNoWrap moves forward without wrapping. moved is false at the last valid slot.
func (r *FocusRing) NextNoWrap() (moved bool) {
	if r.n == 0 {
		return false
	}
	cur := r.idx
	for i := cur + 1; i < r.n; i++ {
		if !r.isSkippable(i) {
			r.idx = i
			return true
		}
	}
	return false
}

// PrevNoWrap moves backward without wrapping. moved is false at the first valid slot.
func (r *FocusRing) PrevNoWrap() (moved bool) {
	if r.n == 0 {
		return false
	}
	for i := r.idx - 1; i >= 0; i-- {
		if !r.isSkippable(i) {
			r.idx = i
			return true
		}
	}
	return false
}
