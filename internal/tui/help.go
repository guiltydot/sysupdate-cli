package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

// openHelp shows the help screen, remembering where to return to.
func (m *Model) openHelp() {
	if m.screen != scrHelp {
		m.prevScreen = m.screen
	}
	m.screen = scrHelp
	m.helpView = viewport.New(m.width-2, max(3, m.height-4))
	m.renderHelp()
}

// renderHelp renders the embedded markdown for the current width.
func (m *Model) renderHelp() {
	text := m.helpText
	if text == "" {
		text = "# sysupdate\n\nNo help text was built into this binary."
	}
	wrap := max(40, min(m.helpView.Width-2, 110))
	style := "light"
	if m.darkBG {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(wrap))
	out := text
	if err == nil {
		if s, err := r.Render(text); err == nil {
			out = strings.TrimRight(s, "\n")
		}
	}
	off := m.helpView.YOffset
	m.helpView.SetContent(out)
	m.helpView.SetYOffset(off)
}

func (m Model) updateHelp(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	km := helpKeys
	switch {
	case key.Matches(k, km.Back):
		m.screen = m.prevScreen
		return m, nil
	case key.Matches(k, km.Top):
		m.helpView.GotoTop()
		return m, nil
	case key.Matches(k, km.Bottom):
		m.helpView.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.helpView, cmd = m.helpView.Update(k)
	return m, cmd
}

func (m Model) viewHelp() string {
	head := sTitle.Render("sysupdate") + "  " + sBold.Render("Help") +
		sMuted.Render(fmt.Sprintf("  %d%%", int(m.helpView.ScrollPercent()*100)))
	return head + "\n" + sBox.Width(m.helpView.Width).Render(m.helpView.View()) + "\n" +
		m.footer(helpKeys)
}
