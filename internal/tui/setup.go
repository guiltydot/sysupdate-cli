package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// The setup screen is shown on the first run (no config file yet) and by
// `sysupdate setup`. It lists every step, detects which tools are installed,
// pre-selects the installed ones that are on by default, and lets the user
// choose before anything is saved.

type setupState struct {
	cursor  int
	sel     map[string]bool
	touched map[string]bool // changed by the user, so detection must not override it
}

func (m *Model) openSetup() {
	m.setup = setupState{sel: map[string]bool{}, touched: map[string]bool{}}
	for _, r := range m.rows {
		if r.probed {
			m.setupProbed(r)
		}
	}
	m.screen = scrSetup
}

// setupProbed sets the default choice for a step once we know if it is installed.
// On a re-run of setup, the current config is the starting point.
func (m *Model) setupProbed(r *row) {
	if m.setup.sel == nil || m.setup.touched[r.step.ID] {
		return
	}
	m.setup.sel[r.step.ID] = r.available && m.enabled(r)
}

func (m Model) setupCounts() (installed, probing, selected int) {
	for _, r := range m.rows {
		switch {
		case !r.probed:
			probing++
		case r.available:
			installed++
			if m.setup.sel[r.step.ID] {
				selected++
			}
		}
	}
	return
}

func (m Model) updateSetup(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	km, st := setupKeys, &m.setup
	switch {
	case key.Matches(k, km.Quit):
		return m, tea.Quit
	case key.Matches(k, km.Up):
		st.cursor = max(0, st.cursor-1)
	case key.Matches(k, km.Down):
		st.cursor = min(len(m.rows)-1, st.cursor+1)
	case key.Matches(k, km.Toggle):
		r := m.rows[st.cursor]
		switch {
		case !r.probed:
			m.setFlash("still detecting " + r.step.Name)
		case !r.available:
			m.setFlash(r.step.Name + " is not installed on this machine")
		default:
			st.sel[r.step.ID] = !st.sel[r.step.ID]
			st.touched[r.step.ID] = true
		}
	case key.Matches(k, km.All):
		all := true
		for _, r := range m.rows {
			if r.available && !st.sel[r.step.ID] {
				all = false
			}
		}
		for _, r := range m.rows {
			if r.available {
				st.sel[r.step.ID], st.touched[r.step.ID] = !all, true
			}
		}
	case key.Matches(k, km.Save):
		// Only installed steps are written: a tool installed later then starts
		// with its default instead of an explicit "off".
		for _, r := range m.rows {
			if r.probed && r.available {
				m.cfg.Enabled[r.step.ID] = st.sel[r.step.ID]
			}
		}
		m.setup = setupState{}
		m.screen = scrOverview
		_, _, n := m.setupCounts()
		return m, m.configChanged(fmt.Sprintf("setup saved: %d step(s) selected", n))
	case key.Matches(k, km.Skip):
		// Keep the defaults, but write the file so setup doesn't come back.
		m.setup = setupState{}
		m.screen = scrOverview
		return m, m.configChanged("using default selection · press s for settings")
	}
	return m, nil
}

func (m Model) viewSetup() string {
	st := m.setup
	var b strings.Builder
	b.WriteString(sTitle.Render("sysupdate") + "  " + sBold.Render("Setup") + "\n\n")
	b.WriteString(sBold.Render("Choose what sysupdate should keep up to date on this machine.") + "\n")
	installed, probing, selected := m.setupCounts()
	status := fmt.Sprintf("%d of %d tools found · %d selected", installed, len(m.rows), selected)
	if probing > 0 {
		status = m.spinner() + " detecting installed tools… " + status
	}
	b.WriteString(sMuted.Render(status) + "\n\n")

	nameW := 22
	for i, r := range m.rows {
		cur := "  "
		if i == st.cursor {
			cur = sAccent.Render("▸ ")
		}
		var box, name, info string
		switch {
		case !r.probed:
			box, name, info = sMuted.Render("[ ]"), sMuted.Render(pad(r.step.Name, nameW)), m.spinner()+sMuted.Render(" detecting")
		case !r.available:
			box, name, info = sMuted.Render(" – "), sMuted.Render(pad(r.step.Name, nameW)), sMuted.Render("not installed")
		default:
			box = "[ ]"
			if st.sel[r.step.ID] {
				box = sOK.Render("[✓]")
			}
			name, info = pad(r.step.Name, nameW), r.version
			if r.step.NeedsSudo {
				info += sMuted.Render("  · uses sudo")
			}
			if !r.step.DefaultOn {
				info += sMuted.Render("  · off by default")
			}
		}
		line := cur + box + " " + name + " " + info
		if i == st.cursor {
			line = sSel.Render(pad(line, m.width-1))
		}
		b.WriteString(line + "\n")
	}

	r := m.rows[st.cursor]
	b.WriteString("\n" + sMuted.PaddingLeft(1).Width(m.width-1).Render(r.step.Description) + "\n")
	b.WriteString("\n" + sMuted.Render(" You can change this any time: space on the main screen, s for settings, or ") +
		sKey.Render("sysupdate setup") + sMuted.Render(".") + "\n")
	if m.flash != "" {
		b.WriteString(" " + sWarn.Render(m.flash) + "\n")
	}

	body := b.String()
	foot := m.footer(setupKeys)
	if gap := m.height - strings.Count(body, "\n") - strings.Count(foot, "\n") - 1; gap > 0 {
		body += strings.Repeat("\n", gap)
	}
	return body + foot
}
