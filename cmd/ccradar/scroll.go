package main

import (
)

// chromeRows is the number of fixed (non-list) lines the view draws:
// tab bar, blank, sticky-header line, blank-before-footer, help, and flash.
func (m model) chromeRows() int {
	n := 5 // tabbar, blank, sticky line, blank-before-footer, help
	if m.flash != "" {
		n++
	}
	return n
}

// visibleRows is how many list rows fit on screen. Returns all rows when the
// window size isn't known yet (e.g. headless render) so nothing is hidden.
func (m model) visibleRows() int {
	if m.height <= 0 {
		return len(m.rows)
	}
	v := m.height - m.chromeRows()
	if v < 1 {
		v = 1
	}
	return v
}

// adjustScroll keeps the cursor inside the visible window.
func (m *model) adjustScroll() {
	v := m.visibleRows()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+v {
		m.top = m.cursor - v + 1
	}
	if maxTop := len(m.rows) - v; m.top > maxTop {
		m.top = maxTop
	}
	if m.top < 0 {
		m.top = 0
	}
	// Don't strand only-header rows above the window: if nothing selectable is
	// hidden above the cursor, pull the top up so those headers reappear (this
	// is what lets you scroll fully back to the top).
	if m.top > 0 && m.cursor < v {
		selectableAbove := false
		for i := 0; i < m.top; i++ {
			if selectable(m.rows[i].kind) {
				selectableAbove = true
				break
			}
		}
		if !selectableAbove {
			m.top = 0
		}
	}
}

func (m *model) clampCursor() {
	defer m.adjustScroll()
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if !selectable(m.rows[m.cursor].kind) {
		for i := m.cursor; i < len(m.rows); i++ {
			if selectable(m.rows[i].kind) {
				m.cursor = i
				return
			}
		}
		for i := m.cursor; i >= 0; i-- {
			if selectable(m.rows[i].kind) {
				m.cursor = i
				return
			}
		}
	}
}

func (m *model) move(delta int) {
	i := m.cursor
	for {
		i += delta
		if i < 0 || i >= len(m.rows) {
			break
		}
		if selectable(m.rows[i].kind) {
			m.cursor = i
			break
		}
	}
	m.adjustScroll()
}
