// Package tui is the Bubble Tea interface for sysupdate.
package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/guiltydot/sysupdate-cli/internal/config"
	"github.com/guiltydot/sysupdate-cli/internal/state"
	"github.com/guiltydot/sysupdate-cli/internal/steps"
)

type screen int

const (
	scrOverview screen = iota
	scrRun
	scrSettings
	scrLogs
	scrHelp
	scrCustom
	scrForm
)

// row is one step as shown in the overview.
type row struct {
	step      *steps.Step
	probed    bool
	available bool
	version   string
	checking  bool
	check     steps.CheckResult
	checked   bool
	preview   []steps.Action
	planErr   error
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg    *config.Config
	cfgErr error
	st     *state.State
	rows   []*row
	gen    int // bumped when rows are rebuilt so stale async results are dropped

	screen        screen
	width, height int
	cursor        int
	flash         string
	flashAt       time.Time
	spin          spinner.Model
	reboot        bool
	darkBG        bool

	helpText   string
	helpView   viewport.Model
	prevScreen screen

	run *runState

	help help.Model

	// huh form currently shown on scrForm, and what to do when it closes.
	form       *huh.Form
	formTitle  string
	formDone   func(m *Model) tea.Cmd // called when the form is submitted
	formReturn screen

	customCursor int
	set          settingsState

	logs    []string
	logIdx  int
	logView viewport.Model
}

// New builds the model from the loaded configuration.
// helpText is the markdown shown on the help screen.
func New(cfg *config.Config, cfgErr error, helpText string, darkBG bool) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Spinner{Frames: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}, FPS: time.Second / 10}), spinner.WithStyle(sAccent))
	m := Model{cfg: cfg, cfgErr: cfgErr, st: state.Load(), spin: sp, width: 100, height: 30, help: newHelp(),
		helpText: helpText, darkBG: darkBG}
	m.buildRows()
	if cfgErr != nil {
		m.setFlash("config error, using defaults: " + cfgErr.Error())
	}
	return m
}

func (m *Model) buildRows() {
	m.gen++
	m.rows = nil
	for _, s := range steps.All(m.cfg) {
		m.rows = append(m.rows, &row{step: s})
	}
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
}

// spinner returns the current spinner frame.
func (m Model) spinner() string { return m.spin.View() }

func (m *Model) setFlash(s string) { m.flash, m.flashAt = s, time.Now() }

func (m Model) enabled(r *row) bool { return m.cfg.IsEnabled(r.step.ID, r.step.DefaultOn) }

// ---- async messages ----

type probeMsg struct {
	gen, idx  int
	available bool
	version   string
	preview   []steps.Action
	planErr   error
}

type checkMsg struct {
	gen, idx int
	res      steps.CheckResult
}

type editorDoneMsg struct{ err error }

type sudoDoneMsg struct {
	err   error
	rows  []int
	steps []*steps.Step
}

type tickMsg time.Time

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.probeAll(), tick()}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) probeAll() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.rows {
		cmds = append(cmds, m.probe(i))
	}
	return tea.Batch(cmds...)
}

func (m Model) probe(i int) tea.Cmd {
	s, gen, cfg := m.rows[i].step, m.gen, m.cfg
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := probeMsg{gen: gen, idx: i, available: s.Available()}
		if msg.available {
			msg.version = s.Version(ctx, cfg)
			msg.preview, msg.planErr = s.Plan(ctx, cfg)
		}
		return msg
	}
}

func (m *Model) checkAll() tea.Cmd {
	var cmds []tea.Cmd
	for i, r := range m.rows {
		if r.available && r.step.Check != nil && !r.checking {
			cmds = append(cmds, m.checkOne(i))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) checkOne(i int) tea.Cmd {
	r := m.rows[i]
	if r.step.Check == nil || !r.available {
		return nil
	}
	r.checking = true
	s, gen, cfg := r.step, m.gen, m.cfg
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return checkMsg{gen: gen, idx: i, res: s.Check(ctx, cfg)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.resizeViewports()
		if m.form != nil {
			m.form = m.form.WithWidth(m.formWidth())
		}
		return m, nil

	case progress.FrameMsg:
		if m.run != nil {
			pm, cmd := m.run.prog.Update(msg)
			m.run.prog = pm.(progress.Model)
			return m, cmd
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		_, err := os.Stat("/var/run/reboot-required")
		m.reboot = err == nil
		if m.flash != "" && time.Since(m.flashAt) > 6*time.Second {
			m.flash = ""
		}
		return m, tick()

	case probeMsg:
		if msg.gen != m.gen || msg.idx >= len(m.rows) {
			return m, nil
		}
		r := m.rows[msg.idx]
		wasProbed := r.probed
		r.probed, r.available, r.version, r.preview, r.planErr = true, msg.available, msg.version, msg.preview, msg.planErr
		if !wasProbed && m.cfg.General.CheckOnStart && r.available && !r.checked {
			return m, m.checkOne(msg.idx)
		}
		return m, nil

	case checkMsg:
		if msg.gen != m.gen || msg.idx >= len(m.rows) {
			return m, nil
		}
		r := m.rows[msg.idx]
		r.checking, r.checked, r.check = false, true, msg.res
		return m, nil

	case editorDoneMsg:
		cfg, err := config.Load()
		m.cfg, m.cfgErr = cfg, err
		if err != nil {
			m.setFlash("config error, using defaults: " + err.Error())
		} else {
			m.setFlash("config reloaded")
		}
		m.buildRows()
		return m, m.probeAll()

	case sudoDoneMsg:
		if msg.err != nil {
			m.setFlash("sudo authentication failed; run cancelled")
			return m, nil
		}
		return m.startRun(msg.rows, msg.steps)

	case runEventMsg, runFinishedMsg:
		return m.updateRunEvent(msg)
	}
	// Anything else (cursor blink, field focus, ...) belongs to an open form.
	if m.screen == scrForm {
		return m.updateForm(msg)
	}
	switch msg := msg.(type) {

	case tea.KeyMsg:
		// Forms get every key first: typing "q" or "?" must not leave the screen.
		if m.screen == scrForm {
			return m.updateForm(msg)
		}
		// Text being typed into a setting must not trigger shortcuts.
		if m.screen == scrSettings && m.set.editing && !key.Matches(msg, keyForce) {
			return m.updateSettings(msg)
		}
		if key.Matches(msg, keyForce) && (m.run == nil || m.run.done != nil) {
			return m, tea.Quit
		}
		if key.Matches(msg, keyHelp) && m.screen != scrHelp {
			m.openHelp()
			return m, nil
		}
		switch m.screen {
		case scrCustom:
			return m.updateCustom(msg)
		case scrSettings:
			return m.updateSettings(msg)
		case scrOverview:
			return m.updateOverview(msg)
		case scrRun:
			return m.updateRun(msg)
		case scrLogs:
			return m.updateLogs(msg)
		case scrHelp:
			return m.updateHelp(msg)
		}

	case tea.MouseMsg:
		switch m.screen {
		case scrForm:
			return m.updateForm(msg)
		case scrRun:
			if m.run != nil {
				var cmd tea.Cmd
				m.run.vp, cmd = m.run.vp.Update(msg)
				m.run.follow = m.run.vp.AtBottom()
				return m, cmd
			}
		case scrLogs:
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			return m, cmd
		case scrHelp:
			var cmd tea.Cmd
			m.helpView, cmd = m.helpView.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) resizeViewports() {
	if m.run != nil {
		w, h := m.runLogSize()
		m.run.vp.Width, m.run.vp.Height = w, h
		m.refreshRunLog()
	}
	m.logView.Width, m.logView.Height = m.width-2, max(3, m.height-5)
	if m.screen == scrHelp {
		m.helpView.Width, m.helpView.Height = m.width-2, max(3, m.height-4)
		m.renderHelp()
	}
}

func (m Model) View() string {
	switch m.screen {
	case scrRun:
		return m.viewRun()
	case scrCustom:
		return m.viewCustom()
	case scrSettings:
		return m.viewSettings()
	case scrForm:
		return m.viewForm()
	case scrLogs:
		return m.viewLogs()
	case scrHelp:
		return m.viewHelp()
	default:
		return m.viewOverview()
	}
}

// openEditor suspends the TUI and opens the config file in $VISUAL/$EDITOR.
func (m Model) openEditor() tea.Cmd {
	if _, err := os.Stat(config.Path()); err != nil {
		_ = m.cfg.Save()
	}
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "nano"
		if !steps.Have(ed) {
			ed = "vi"
		}
	}
	c := exec.Command("sh", "-c", ed+` ""`, "sh", config.Path()) //nolint:gosec //  may hold arguments, run through sh like git does; the path is passed as
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// footer renders a key map's short help, wrapped over as many lines as the width needs.
func (m Model) footer(km help.KeyMap) string {
	var enabled []key.Binding
	for _, b := range km.ShortHelp() {
		if b.Enabled() {
			enabled = append(enabled, b)
		}
	}
	h := m.help
	h.Width = 0 // measure without truncation
	var lines []string
	start := 0
	for start < len(enabled) {
		end := start + 1
		for end < len(enabled) && lipgloss.Width(h.ShortHelpView(enabled[start:end+1])) <= m.width {
			end++
		}
		lines = append(lines, h.ShortHelpView(enabled[start:end]))
		start = end
	}
	return strings.Join(lines, "\n")
}

// Run starts the TUI. The background is detected before Bubble Tea takes over the terminal.
func Run(cfg *config.Config, cfgErr error, helpText string) error {
	dark := lipgloss.HasDarkBackground()
	p := tea.NewProgram(New(cfg, cfgErr, helpText, dark), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
