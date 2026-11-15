package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/guiltydot/sysupdate-cli/internal/runner"
)

// ---- log viewer ----

func (m *Model) openLogs() {
	m.logs = runner.Logs()
	m.logIdx = 0
	m.logView = viewport.New(m.width-2, max(3, m.height-5))
	m.loadLog()
	m.screen = scrLogs
}

func (m *Model) loadLog() {
	if len(m.logs) == 0 {
		m.logView.SetContent(sMuted.Render("No run logs yet."))
		return
	}
	content := runner.ReadLog(m.logs[m.logIdx])
	var out []string
	for _, l := range strings.Split(content, "\n") {
		l = strings.ReplaceAll(l, "\t", "    ")
		switch {
		case strings.HasPrefix(l, "=== "):
			l = sSection.Render(l)
		case strings.HasPrefix(l, "$ "):
			l = sAccent.Render(trunc(l, m.logView.Width))
		case strings.HasPrefix(l, "!! "), strings.Contains(l, ": failed ("):
			l = sErr.Render(trunc(l, m.logView.Width))
		case strings.HasPrefix(l, "--- "):
			l = sMuted.Render(l)
		default:
			l = trunc(l, m.logView.Width)
		}
		out = append(out, l)
	}
	m.logView.SetContent(strings.Join(out, "\n"))
	m.logView.GotoTop()
}

func (m Model) updateLogs(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	km := logKeys
	switch {
	case key.Matches(k, km.Back):
		if m.run != nil {
			m.screen = scrRun
		} else {
			m.screen = scrOverview
		}
		return m, nil
	case key.Matches(k, km.Older):
		if m.logIdx < len(m.logs)-1 {
			m.logIdx++
			m.loadLog()
		}
		return m, nil
	case key.Matches(k, km.Newer):
		if m.logIdx > 0 {
			m.logIdx--
			m.loadLog()
		}
		return m, nil
	case key.Matches(k, km.Bottom):
		m.logView.GotoBottom()
		return m, nil
	case key.Matches(k, km.Top):
		m.logView.GotoTop()
		return m, nil
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(k)
	return m, cmd
}

func (m Model) viewLogs() string {
	title := "Logs"
	if len(m.logs) > 0 {
		title = fmt.Sprintf("Log %d/%d  %s", m.logIdx+1, len(m.logs), filepath.Base(m.logs[m.logIdx]))
	}
	head := sTitle.Render("sysupdate") + "  " + sBold.Render(title) +
		sMuted.Render(fmt.Sprintf("  %d%%", int(m.logView.ScrollPercent()*100)))
	return head + "\n" + sBox.Width(m.logView.Width).Render(m.logView.View()) + "\n" +
		m.footer(logKeys)
}
