package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/guiltydot/sysupdate-cli/internal/runner"
	"github.com/guiltydot/sysupdate-cli/internal/state"
	"github.com/guiltydot/sysupdate-cli/internal/steps"
)

const maxLogLines = 5000

type stepRun struct {
	rowIdx  int
	step    *steps.Step
	running bool
	done    bool
	result  runner.Result
	action  string
	actions int
	actDone int
	lines   []string
	started time.Time
}

type runState struct {
	items   []*stepRun
	sel     int
	follow  bool
	autoSel bool // selection follows the running step
	vp      viewport.Model
	prog    progress.Model
	cancel  context.CancelFunc
	events  chan any
	started time.Time
	done    *runner.RunDone
}

type runEventMsg struct{ ev any }
type runFinishedMsg struct{ done runner.RunDone }

func waitEvent(ch chan any) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		if d, ok := ev.(runner.RunDone); ok {
			return runFinishedMsg{d}
		}
		return runEventMsg{ev}
	}
}

func (m Model) startRun(rows []int, sel []*steps.Step) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	rs := &runState{follow: true, autoSel: true, cancel: cancel, events: make(chan any, 4096), started: time.Now()}
	for i, s := range sel {
		rs.items = append(rs.items, &stepRun{rowIdx: rows[i], step: s})
	}
	w, h := m.runLogSize()
	rs.vp = viewport.New(w, h)
	rs.prog = progress.New(progress.WithScaledGradient("#7D6BFF", "#5FD787"), progress.WithoutPercentage())
	m.run = rs
	m.screen = scrRun

	cfg := m.cfg
	go func() {
		done := runner.Run(ctx, cfg, sel, func(ev any) { rs.events <- ev })
		rs.events <- done
		close(rs.events)
	}()
	return m, waitEvent(rs.events)
}

func (m Model) updateRunEvent(msg tea.Msg) (tea.Model, tea.Cmd) {
	rs := m.run
	if rs == nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case runFinishedMsg:
		rs.done = &msg.done
		m.st = state.Load()
		m.reboot = msg.done.RebootRequired
		// Versions changed: re-probe and re-check what ran.
		var cmds []tea.Cmd
		for _, it := range rs.items {
			cmds = append(cmds, m.probe(it.rowIdx))
			m.rows[it.rowIdx].checked = false
			if c := m.checkOne(it.rowIdx); c != nil {
				cmds = append(cmds, c)
			}
		}
		return m, tea.Batch(cmds...)

	case runEventMsg:
		switch ev := msg.ev.(type) {
		case runner.StepStart:
			it := rs.items[ev.Idx]
			it.running, it.started, it.actions = true, time.Now(), len(ev.Actions)
			if rs.autoSel {
				rs.sel = ev.Idx
				rs.follow = true
			}
		case runner.ActionStart:
			it := rs.items[ev.Idx]
			if it.action != "" {
				it.actDone++
			}
			it.action = ev.Label
		case runner.Line:
			it := rs.items[ev.Idx]
			if ev.Replace && len(it.lines) > 0 {
				it.lines[len(it.lines)-1] = ev.Text
			} else {
				it.lines = append(it.lines, ev.Text)
				if len(it.lines) > maxLogLines {
					it.lines = it.lines[len(it.lines)-maxLogLines:]
				}
			}
		case runner.StepDone:
			it := rs.items[ev.Result.Idx]
			it.running, it.done, it.result, it.action = false, true, ev.Result, ""
			if it.actions > 0 {
				it.actDone = it.actions
			}
			if ev.Result.Status == runner.Skipped || ev.Result.Status == runner.Cancelled {
				it.lines = append(it.lines, "("+ev.Result.Err+")")
			}
			if idx := eventIdx(msg.ev); idx == rs.sel {
				m.refreshRunLog()
			}
			return m, tea.Batch(waitEvent(rs.events), rs.prog.SetPercent(rs.fraction()))
		}
		if idx := eventIdx(msg.ev); idx == rs.sel {
			m.refreshRunLog()
		}
		return m, waitEvent(rs.events)
	}
	return m, nil
}

// fraction is the share of steps that have finished.
func (rs *runState) fraction() float64 {
	n := 0
	for _, it := range rs.items {
		if it.done {
			n++
		}
	}
	return float64(n) / float64(max(1, len(rs.items)))
}

func eventIdx(ev any) int {
	switch e := ev.(type) {
	case runner.StepStart:
		return e.Idx
	case runner.ActionStart:
		return e.Idx
	case runner.Line:
		return e.Idx
	case runner.StepDone:
		return e.Result.Idx
	}
	return -1
}

func (m *Model) refreshRunLog() {
	rs := m.run
	if rs == nil || rs.sel >= len(rs.items) {
		return
	}
	lines := rs.items[rs.sel].lines
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "$ "):
			out[i] = sAccent.Render(trunc(l, rs.vp.Width))
		case strings.HasPrefix(l, "!! "):
			out[i] = sErr.Render(trunc(l, rs.vp.Width))
		default:
			out[i] = trunc(strings.ReplaceAll(l, "\t", "    "), rs.vp.Width)
		}
	}
	rs.vp.SetContent(strings.Join(out, "\n"))
	if rs.follow {
		rs.vp.GotoBottom()
	}
}

const (
	runListW   = 30
	runFooterH = 3 // summary, log path, keys
)

func (m Model) runLogSize() (int, int) {
	return max(20, m.width-runListW-5), max(3, m.height-1-3-runFooterH)
}

func (m Model) updateRun(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	rs := m.run
	km := newRunKeys(rs.done != nil)
	switch {
	case key.Matches(k, km.Cancel):
		rs.cancel()
		m.setFlash("cancelling…")
	case key.Matches(k, km.Up):
		if rs.sel > 0 {
			rs.sel--
			rs.autoSel, rs.follow = false, true
			m.refreshRunLog()
		}
	case key.Matches(k, km.Down):
		if rs.sel < len(rs.items)-1 {
			rs.sel++
			rs.autoSel, rs.follow = false, true
			m.refreshRunLog()
		}
	case key.Matches(k, km.Follow):
		rs.autoSel, rs.follow = true, true
		for i, it := range rs.items {
			if it.running {
				rs.sel = i
			}
		}
		m.refreshRunLog()
	case key.Matches(k, km.Scroll):
		var cmd tea.Cmd
		rs.vp, cmd = rs.vp.Update(k)
		rs.follow = rs.vp.AtBottom()
		return m, cmd
	case key.Matches(k, km.Top):
		rs.vp.GotoTop()
		rs.follow = false
	case key.Matches(k, km.Bottom):
		rs.vp.GotoBottom()
		rs.follow = true
	case key.Matches(k, km.Retry):
		var rows []int
		var retry []*steps.Step
		for _, it := range rs.items {
			if it.result.Status == runner.Failed {
				rows, retry = append(rows, it.rowIdx), append(retry, it.step)
			}
		}
		if len(retry) == 0 {
			m.setFlash("no failed steps to retry")
			return m, nil
		}
		return m.requestSteps(rows, retry)
	case key.Matches(k, km.Back):
		m.screen = scrOverview
		m.run = nil
	case key.Matches(k, km.Logs):
		m.openLogs()
	case key.Matches(k, km.Quit):
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewRun() string {
	rs := m.run
	if rs == nil {
		return ""
	}
	finished := rs.done != nil
	nDone, nFail := 0, 0
	for _, it := range rs.items {
		if it.done {
			nDone++
			if it.result.Status == runner.Failed {
				nFail++
			}
		}
	}

	// Header with progress bar
	elapsed := time.Since(rs.started)
	if finished {
		var total time.Duration
		for _, it := range rs.items {
			total += it.result.Duration
		}
		elapsed = total
	}
	prog := rs.prog
	prog.Width = max(10, min(40, m.width-60))
	bar := prog.View()
	state := m.spinner() + " running"
	if finished {
		state = "finished"
	}
	header := sTitle.Render("sysupdate") + "  " + state + fmt.Sprintf("  %d/%d  ", nDone, len(rs.items)) + bar + "  " + sMuted.Render(dur(elapsed))

	// Step list
	var list []string
	for i, it := range rs.items {
		icon, extra := sMuted.Render("·"), ""
		switch {
		case it.running:
			icon = m.spinner()
			if it.actions > 1 {
				extra = fmt.Sprintf(" %d/%d", it.actDone+1, it.actions)
			} else {
				extra = " " + dur(time.Since(it.started))
			}
		case it.done:
			switch it.result.Status {
			case runner.OK:
				icon = sOK.Render("✓")
			case runner.Failed:
				icon = sErr.Render("✗")
			case runner.Skipped:
				icon = sMuted.Render("↷")
			case runner.Cancelled:
				icon = sWarn.Render("■")
			}
			extra = " " + dur(it.result.Duration)
		}
		name := pad(it.step.Name, runListW-4-lipgloss.Width(extra))
		line := icon + " " + name + sMuted.Render(extra)
		if i == rs.sel {
			line = sSel.Render(pad(line, runListW-2))
		}
		list = append(list, line)
		if it.running && it.action != "" {
			list = append(list, sMuted.Render("  └ "+trunc(it.action, runListW-6)))
		}
	}
	h := rs.vp.Height
	left := sBox.Width(runListW - 2).Height(h + 1).Render(strings.Join(list, "\n"))

	title := rs.items[rs.sel].step.Name
	if !rs.follow {
		title += sMuted.Render(fmt.Sprintf("  (scrolled %d%%)", int(rs.vp.ScrollPercent()*100)))
	}
	right := sBox.Width(rs.vp.Width).Height(h).Render(sSection.Render(trunc(title, rs.vp.Width)) + "\n" + rs.vp.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	// Summary / footer
	var foot string
	if finished {
		summary := sOK.Render(fmt.Sprintf("✓ %d step(s) finished without errors", nDone))
		if nFail > 0 {
			var names []string
			for _, it := range rs.items {
				if it.result.Status == runner.Failed {
					names = append(names, it.step.Name)
				}
			}
			summary = sErr.Render(fmt.Sprintf("✗ %d failed: %s", nFail, strings.Join(names, ", ")))
		}
		if rs.done.Cancelled {
			summary = sWarn.Render("■ run cancelled")
		}
		if rs.done.RebootRequired {
			summary += "  " + sWarn.Render("⚠ restart required: run 'wsl --shutdown' from Windows")
		}
		if rs.done.LogPath != "" {
			summary += "\n" + sMuted.Render("log: "+rs.done.LogPath)
		}
		foot = summary + "\n" + m.footer(newRunKeys(true))
	} else {
		foot = m.footer(newRunKeys(false))
		if m.flash != "" {
			foot = sAccent.Render(m.flash) + "\n" + foot
		}
	}
	// Keep the footer a fixed height so the boxes never push the header off screen.
	fl := strings.Split(foot, "\n")
	for i := range fl {
		fl[i] = trunc(fl[i], m.width)
	}
	for len(fl) < runFooterH {
		fl = append([]string{""}, fl...)
	}
	return header + "\n" + body + "\n" + strings.Join(fl, "\n")
}
