package tui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

// The settings screen shows every setting on one page. Toggles and choices
// change with space or ←/→, text values are edited in place with enter.
// Every change is saved straight away.

type settingKind int

const (
	kindBool settingKind = iota
	kindChoice
	kindText
)

type choice struct{ value, label string }

type setting struct {
	section, title, desc string
	kind                 settingKind
	flag                 func(c *config.Config) *bool // kindBool
	choices              []choice                     // kindChoice
	get                  func(c *config.Config) string
	set                  func(c *config.Config, v string) error // kindChoice, kindText
	placeholder          string
}

func joinList(l []string) string { return strings.Join(l, ", ") }

func splitList(s string) []string {
	return append([]string{}, strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })...)
}

func listSetting(section, title, desc string, field func(c *config.Config) *[]string) setting {
	return setting{
		section: section, title: title, desc: desc, kind: kindText, placeholder: "none",
		get: func(c *config.Config) string { return joinList(*field(c)) },
		set: func(c *config.Config, v string) error { *field(c) = splitList(v); return nil },
	}
}

var settingsList = []setting{
	{section: "Ubuntu packages (APT)", title: "full-upgrade", kind: kindBool,
		desc: "Use apt-get full-upgrade, which may add or remove packages to complete an upgrade, instead of upgrade.",
		flag: func(c *config.Config) *bool { return &c.Apt.FullUpgrade }},
	{section: "Ubuntu packages (APT)", title: "autoremove --purge after upgrading", kind: kindBool,
		desc: "Remove packages that are no longer needed after every run. Off: they are only reported, and you can remove them with d.",
		flag: func(c *config.Config) *bool { return &c.Apt.Autoremove }},
	{section: "Ubuntu packages (APT)", title: "autoclean", kind: kindBool,
		desc: "Delete obsolete .deb files from the package cache after upgrading.",
		flag: func(c *config.Config) *bool { return &c.Apt.Autoclean }},

	{section: "Toolchains", title: "Node.js version to track", kind: kindChoice,
		desc:    "Which release the Node step installs.",
		choices: []choice{{"current-major", "newest of my current major"}, {"lts", "latest LTS"}},
		get:     func(c *config.Config) string { return c.Node.Channel },
		set:     func(c *config.Config, v string) error { c.Node.Channel = v; return nil }},
	{section: "Toolchains", title: "Reinstall global npm packages", kind: kindBool,
		desc: "Carry global npm packages over when nvm installs a new Node version.",
		flag: func(c *config.Config) *bool { return &c.Node.ReinstallPackages }},
	{section: "Toolchains", title: "Go install root", kind: kindText,
		desc: "Where the Go step installs Go. Must be an absolute path.",
		get:  func(c *config.Config) string { return c.Go.Root },
		set: func(c *config.Config, v string) error {
			v = strings.TrimSpace(v)
			if !strings.HasPrefix(v, "/") {
				return errors.New("must be an absolute path")
			}
			c.Go.Root = strings.TrimRight(v, "/")
			return nil
		}},

	listSetting("Exclusions", "Cargo crates never reinstalled", "Comma-separated crate names.",
		func(c *config.Config) *[]string { return &c.Cargo.Exclude }),
	listSetting("Exclusions", "Go binaries gup never updates", "Comma-separated binary names from $GOBIN.",
		func(c *config.Config) *[]string { return &c.Gup.Exclude }),
	listSetting("Exclusions", "Docker images never pulled", "Comma-separated, as repo:tag or just repo.",
		func(c *config.Config) *[]string { return &c.Docker.Exclude }),

	{section: "General", title: "Stop on first failure", kind: kindBool,
		desc: "Skip the remaining steps after one fails.",
		flag: func(c *config.Config) *bool { return &c.General.StopOnFailure }},
	{section: "General", title: "Check for updates on start", kind: kindBool,
		desc: "Look for pending updates when the app opens. This uses the network.",
		flag: func(c *config.Config) *bool { return &c.General.CheckOnStart }},
	{section: "General", title: "Run logs to keep", kind: kindText,
		desc: "How many run logs to keep in ~/.local/state/sysupdate/logs.",
		get:  func(c *config.Config) string { return strconv.Itoa(c.General.KeepLogs) },
		set: func(c *config.Config, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 {
				return errors.New("enter a whole number of at least 1")
			}
			c.General.KeepLogs = n
			return nil
		}},
}

type settingsState struct {
	cursor  int
	editing bool
	input   textinput.Model
	err     string
	changed bool
}

func (m *Model) openSettings() tea.Cmd {
	m.set = settingsState{}
	m.screen = scrSettings
	return nil
}

// saveSetting writes the config after a change. Rows are rebuilt when the screen closes.
func (m *Model) saveSetting() {
	m.set.changed = true
	if err := m.cfg.Save(); err != nil {
		m.set.err = "could not save config: " + err.Error()
	}
}

func (m Model) updateSettings(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := &m.set
	s := settingsList[st.cursor]
	if st.editing {
		switch k.Type {
		case tea.KeyEsc:
			st.editing, st.err = false, ""
		case tea.KeyEnter:
			if err := s.set(m.cfg, st.input.Value()); err != nil {
				st.err = err.Error()
				return m, nil
			}
			st.editing, st.err = false, ""
			m.saveSetting()
		default:
			var cmd tea.Cmd
			st.input, cmd = st.input.Update(k)
			return m, cmd
		}
		return m, nil
	}

	km := settingsKeys
	st.err = ""
	switch {
	case key.Matches(k, km.Back):
		m.screen = scrOverview
		if st.changed {
			return m, m.configChanged("settings saved")
		}
	case key.Matches(k, km.Up):
		st.cursor = (st.cursor + len(settingsList) - 1) % len(settingsList)
	case key.Matches(k, km.Down):
		st.cursor = (st.cursor + 1) % len(settingsList)
	case key.Matches(k, km.Top):
		st.cursor = 0
	case key.Matches(k, km.Bottom):
		st.cursor = len(settingsList) - 1
	case key.Matches(k, km.Change), key.Matches(k, km.Left), key.Matches(k, km.Right):
		switch s.kind {
		case kindBool:
			p := s.flag(m.cfg)
			*p = !*p
			m.saveSetting()
		case kindChoice:
			cur := 0
			for i, c := range s.choices {
				if c.value == s.get(m.cfg) {
					cur = i
				}
			}
			step := 1
			if key.Matches(k, km.Left) {
				step = len(s.choices) - 1
			}
			_ = s.set(m.cfg, s.choices[(cur+step)%len(s.choices)].value)
			m.saveSetting()
		case kindText:
			if key.Matches(k, km.Change) {
				in := textinput.New()
				in.Prompt = ""
				in.Placeholder = s.placeholder
				in.SetValue(s.get(m.cfg))
				in.CursorEnd()
				in.Cursor.SetMode(cursor.CursorStatic)
				in.Focus()
				st.input, st.editing = in, true
			}
		}
	}
	return m, nil
}

func (m Model) viewSettings() string {
	st := m.set
	var b strings.Builder
	b.WriteString(sTitle.Render("sysupdate") + "  " + sBold.Render("Settings") +
		sMuted.Render("  ·  changes are saved immediately") + "\n")

	const labelW = 36
	valueW := max(10, m.width-labelW-6)
	section := ""
	for i, s := range settingsList {
		if s.section != section {
			section = s.section
			b.WriteString("\n " + sSection.Render(section) + "\n")
		}
		sel := i == st.cursor
		marker := "  "
		if sel {
			marker = sAccent.Render("▸ ")
		}
		var val string
		switch {
		case sel && st.editing:
			st.input.Width = valueW - 2
			val = sSel.Render(" " + st.input.View() + " ")
		case s.kind == kindBool:
			if *s.flag(m.cfg) {
				val = sOK.Render("● on")
			} else {
				val = sMuted.Render("○ off")
			}
		case s.kind == kindChoice:
			label := s.get(m.cfg)
			for _, c := range s.choices {
				if c.value == label {
					label = c.label
				}
			}
			val = sAccent.Render("‹ ") + label + sAccent.Render(" ›")
		default:
			v := s.get(m.cfg)
			if v == "" {
				val = sMuted.Render(s.placeholder)
			} else {
				val = trunc(v, valueW)
			}
		}
		title := pad(s.title, labelW)
		if sel {
			title = sBold.Render(title)
		}
		b.WriteString(" " + marker + title + val + "\n")
	}

	s := settingsList[st.cursor]
	b.WriteString("\n" + sMuted.PaddingLeft(1).Width(m.width-1).Render(s.desc) + "\n")
	if st.err != "" {
		b.WriteString(" " + sErr.Render(st.err) + "\n")
	} else {
		b.WriteString("\n")
	}

	// Pad so the footer sits at the bottom of the window.
	body := b.String()
	foot := m.footer(m.settingsKeyMap())
	if gap := m.height - strings.Count(body, "\n") - strings.Count(foot, "\n") - 1; gap > 0 {
		body += strings.Repeat("\n", gap)
	}
	return body + foot
}

// settingsKeyMap shows only the keys that apply to the highlighted setting.
func (m Model) settingsKeyMap() settingsKeyMap {
	k := settingsKeys
	if m.set.editing {
		return settingsKeyMap{
			Change: bind([]string{"enter"}, "enter", "save"),
			Back:   bind([]string{"esc"}, "esc", "cancel edit"),
		}
	}
	switch settingsList[m.set.cursor].kind {
	case kindBool:
		k.Change.SetHelp("space/enter", "toggle")
	case kindChoice:
		k.Change.SetHelp("←/→", "change")
	case kindText:
		k.Change.SetHelp("enter", "edit")
	}
	return k
}
