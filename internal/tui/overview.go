package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/guiltydot/sysupdate-cli/internal/runner"
	"github.com/guiltydot/sysupdate-cli/internal/steps"
)

func (m Model) updateOverview(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	km := overviewKeys
	switch {
	case key.Matches(k, km.Quit):
		return m, tea.Quit
	case key.Matches(k, km.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(k, km.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case key.Matches(k, km.Top):
		m.cursor = 0
	case key.Matches(k, km.Bottom):
		m.cursor = len(m.rows) - 1
	case key.Matches(k, km.Toggle):
		r := m.rows[m.cursor]
		m.cfg.Enabled[r.step.ID] = !m.enabled(r)
		m.save()
	case key.Matches(k, km.All):
		// Enable all installed steps, or disable all if they already are.
		all := true
		for _, r := range m.rows {
			if r.available && !m.enabled(r) {
				all = false
			}
		}
		for _, r := range m.rows {
			m.cfg.Enabled[r.step.ID] = !all
		}
		m.save()
	case key.Matches(k, km.Outdated):
		// Select exactly the steps that have pending updates.
		n := 0
		for _, r := range m.rows {
			on := r.checked && r.check.State == steps.CheckOutdated
			m.cfg.Enabled[r.step.ID] = on
			if on {
				n++
			}
		}
		m.save()
		m.setFlash(fmt.Sprintf("selected %d step(s) with updates", n))
	case key.Matches(k, km.Check):
		m.setFlash("checking for updates…")
		return m, m.checkAll()
	case key.Matches(k, km.CheckOne):
		m.setFlash("checking " + m.rows[m.cursor].step.Name + "…")
		return m, m.checkOne(m.cursor)
	case key.Matches(k, km.Run):
		var list []int
		for i, r := range m.rows {
			if r.available && m.enabled(r) {
				list = append(list, i)
			}
		}
		if len(list) == 0 {
			m.setFlash("nothing selected — press space to enable steps")
			return m, nil
		}
		return m.requestRun(list)
	case key.Matches(k, km.RunOne):
		r := m.rows[m.cursor]
		if !r.available {
			m.setFlash(r.step.Name + " is not installed")
			return m, nil
		}
		return m.requestRun([]int{m.cursor})
	case key.Matches(k, km.Clean):
		return m, m.confirmCleanup(m.cursor)
	case key.Matches(k, km.Settings):
		return m, m.openSettings()
	case key.Matches(k, km.Custom):
		m.screen = scrCustom
	case key.Matches(k, km.Edit):
		return m, m.openEditor()
	case key.Matches(k, km.Logs):
		m.openLogs()
	}
	return m, nil
}

func (m *Model) save() {
	if err := m.cfg.Save(); err != nil {
		m.setFlash("could not save config: " + err.Error())
	}
}

// requestRun makes sure sudo is authenticated (prompting outside the TUI) before running.
func (m Model) requestRun(list []int) (tea.Model, tea.Cmd) {
	var sel []*steps.Step
	for _, i := range list {
		sel = append(sel, m.rows[i].step)
	}
	return m.requestSteps(list, sel)
}

// requestSteps runs sel; rows[i] is the overview row sel[i] belongs to, which is
// re-probed and re-checked afterwards. sudo is asked for first when needed.
func (m Model) requestSteps(rows []int, sel []*steps.Step) (tea.Model, tea.Cmd) {
	if runner.NeedsSudo(sel) && !runner.SudoCached() {
		c := exec.Command("sh", "-c", `echo "sysupdate needs sudo for: $1"; sudo -v`, "sh", sudoNames(sel))
		return m, tea.ExecProcess(c, func(err error) tea.Msg { return sudoDoneMsg{err: err, rows: rows, steps: sel} })
	}
	return m.startRun(rows, sel)
}

// confirmCleanup asks before removing the highlighted step's leftovers.
func (m *Model) confirmCleanup(i int) tea.Cmd {
	r := m.rows[i]
	if !r.checked || len(r.check.Cleanup) == 0 {
		m.setFlash("nothing to clean up for " + r.step.Name + " (press C to check it)")
		return nil
	}
	items := r.check.Cleanup
	var desc []string
	for _, c := range items {
		desc = append(desc, "• "+c.Text+"\n    $ "+c.Action.Display)
	}
	ok := true
	f := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Remove these?").Description(strings.Join(desc, "\n")).
			Affirmative("Remove").Negative("Cancel").Value(&ok),
	))
	return m.showForm("Clean up "+r.step.Name, f, func(m *Model) tea.Cmd {
		if !ok {
			m.setFlash("nothing removed")
			return nil
		}
		nm, cmd := m.requestSteps([]int{i}, []*steps.Step{steps.CleanupStep(r.step, items)})
		*m = nm.(Model)
		return cmd
	})
}

func sudoNames(list []*steps.Step) string {
	var n []string
	for _, s := range list {
		if s.NeedsSudo {
			n = append(n, s.Name)
		}
	}
	return strings.Join(n, ", ")
}

func (m Model) statusCell(r *row) string {
	switch {
	case !r.probed:
		return m.spinner() + sMuted.Render(" probing")
	case !r.available:
		return sMuted.Render("not installed")
	case r.checking:
		return m.spinner() + sMuted.Render(" checking")
	case !r.checked:
		if r.step.Check == nil {
			return sMuted.Render("—")
		}
		return sMuted.Render("not checked")
	}
	extra := ""
	if n := len(r.check.Cleanup); n > 0 {
		extra = sMuted.Render(fmt.Sprintf(" · %d to clean", n))
	}
	switch r.check.State {
	case steps.CheckUpToDate:
		return sOK.Render("✓ "+r.check.Summary) + extra
	case steps.CheckOutdated:
		return sWarn.Render("↑ "+r.check.Summary) + extra
	case steps.CheckError:
		return sErr.Render("✗ " + r.check.Summary)
	}
	return sMuted.Render("?")
}

func (m Model) lastRunCell(r *row) string {
	s, ok := m.st.Steps[r.step.ID]
	if !ok {
		return sMuted.Render("never")
	}
	mark := sOK.Render("✓")
	if !s.OK {
		mark = sErr.Render("✗")
	}
	return mark + " " + ago(s.LastRun) + sMuted.Render(" ("+dur(s.Duration)+")")
}

func (m Model) viewOverview() string {
	var b strings.Builder
	w := m.width

	// Header
	pending, enabled := 0, 0
	for _, r := range m.rows {
		if r.checked && r.check.State == steps.CheckOutdated {
			pending++
		}
		if r.available && m.enabled(r) {
			enabled++
		}
	}
	info := []string{"last run " + ago(m.st.LastRun)}
	if pending > 0 {
		info = append(info, sWarn.Render(fmt.Sprintf("%d with updates", pending)))
	}
	if m.reboot {
		info = append(info, sErr.Render("⚠ reboot required (wsl --shutdown)"))
	}
	b.WriteString(sTitle.Render("sysupdate") + "  " + sMuted.Render(strings.Join(info, " · ")) + "\n\n")

	// Table
	nameW, verW, lastW := 20, 22, 22
	statW := max(14, w-4-4-nameW-verW-lastW-6)
	head := "    " + "    " + pad("STEP", nameW) + "  " + pad("VERSION", verW) + "  " + pad("UPDATES", statW) + "  " + pad("LAST RUN", lastW)
	b.WriteString(sHeader.Render(trunc(head, w)) + "\n")
	for i, r := range m.rows {
		cur := "  "
		if i == m.cursor {
			cur = sAccent.Render("▸ ")
		}
		box := "[ ]"
		switch {
		case r.probed && !r.available:
			box = sMuted.Render(" – ")
		case m.enabled(r):
			box = sOK.Render("[✓]")
		}
		sudo := "  "
		if r.step.NeedsSudo {
			sudo = sMuted.Render("# ")
		}
		name := r.step.Name
		if r.probed && !r.available {
			name = sMuted.Render(name)
		}
		line := cur + box + " " + sudo + pad(name, nameW) + "  " + pad(sMuted.Render(r.version), verW) + "  " +
			pad(m.statusCell(r), statW) + "  " + pad(m.lastRunCell(r), lastW)
		line = trunc(line, w)
		if i == m.cursor {
			line = sSel.Render(pad(line, w))
		}
		b.WriteString(line + "\n")
	}

	// Footer (computed first so the detail pane can take the remaining height)
	km := overviewKeys
	km.Run.SetHelp("enter", fmt.Sprintf("run %d selected", enabled))
	footer := m.footer(km)
	if m.flash != "" {
		footer = sAccent.Render(trunc(m.flash, w)) + "\n" + footer
	}

	used := 2 + 1 + len(m.rows) + 1 + lipgloss.Height(footer)
	detailH := m.height - used - 1
	if detailH >= 3 && len(m.rows) > 0 {
		b.WriteString("\n" + m.viewDetail(m.rows[m.cursor], w, detailH))
	}
	out := b.String()
	if gap := m.height - lipgloss.Height(out) - lipgloss.Height(footer); gap > 0 {
		out += strings.Repeat("\n", gap)
	}
	return out + footer
}

func (m Model) viewDetail(r *row, w, h int) string {
	var lines []string
	title := sSection.Render(r.step.Name)
	if r.step.NeedsSudo {
		title += sMuted.Render("  (uses sudo)")
	}
	lines = append(lines, title, sMuted.Render(r.step.Description))
	if r.probed && r.available {
		if r.planErr != nil {
			lines = append(lines, sErr.Render("Cannot plan: "+r.planErr.Error()))
		} else if len(r.preview) > 0 {
			lines = append(lines, "", sHeader.Render("Will run:"))
			for _, a := range r.preview {
				lines = append(lines, "  "+sMuted.Render("$ ")+a.Display)
			}
		}
	}
	if r.checked && len(r.check.Details) > 0 {
		head := "Pending:"
		if r.check.State != steps.CheckOutdated {
			head = "Notes:"
		}
		lines = append(lines, "", sHeader.Render(head))
		for _, d := range r.check.Details {
			lines = append(lines, "  "+d)
		}
	}
	if r.checked && len(r.check.Cleanup) > 0 {
		lines = append(lines, "", sHeader.Render("Could be removed")+sMuted.Render(" (press ")+sKey.Render("d")+sMuted.Render(" to remove)"))
		for _, c := range r.check.Cleanup {
			lines = append(lines, "  "+sWarn.Render("• ")+c.Text+"  "+sMuted.Render("$ "+c.Action.Display))
		}
	}
	if st, ok := m.st.Steps[r.step.ID]; ok && !st.LastOK.IsZero() && !st.OK {
		lines = append(lines, "", sErr.Render("Last run failed; last success "+ago(st.LastOK)))
	}
	if len(lines) > h {
		extra := len(lines) - h + 1
		lines = append(lines[:h-1], sMuted.Render(fmt.Sprintf("  … %d more", extra)))
	}
	for i := range lines {
		lines[i] = trunc(lines[i], w-2)
	}
	return strings.Join(lines, "\n") + "\n"
}
