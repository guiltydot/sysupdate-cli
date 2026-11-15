package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/guiltydot/sysupdate-cli/internal/config"
	"github.com/guiltydot/sysupdate-cli/internal/steps"
)

// ---- form plumbing ----

func (m Model) formWidth() int { return max(40, min(m.width-4, 90)) }

// showForm opens f on the form screen; done runs when it is submitted.
func (m *Model) showForm(title string, f *huh.Form, done func(m *Model) tea.Cmd) tea.Cmd {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	m.form = f.WithTheme(huh.ThemeCharm()).WithKeyMap(km).WithShowHelp(true).WithWidth(m.formWidth())
	m.formTitle, m.formDone = title, done
	if m.screen != scrForm {
		m.formReturn = m.screen
	}
	m.screen = scrForm
	return m.form.Init()
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.form == nil {
		m.screen = scrOverview
		return m, nil
	}
	fm, cmd := m.form.Update(msg)
	m.form = fm.(*huh.Form)
	switch m.form.State {
	case huh.StateCompleted:
		done := m.formDone
		m.closeForm()
		if done != nil {
			return m, tea.Batch(cmd, done(&m))
		}
	case huh.StateAborted:
		m.closeForm()
		m.setFlash("cancelled, nothing changed")
	}
	return m, cmd
}

func (m *Model) closeForm() {
	m.screen = m.formReturn
	m.form, m.formDone = nil, nil
}

func (m Model) viewForm() string {
	if m.form == nil {
		return ""
	}
	return sTitle.Render("sysupdate") + "  " + sBold.Render(m.formTitle) + "\n\n" + m.form.View()
}

// configChanged saves the config and refreshes the step list.
func (m *Model) configChanged(flash string) tea.Cmd {
	if err := m.cfg.Save(); err != nil {
		m.setFlash("could not save config: " + err.Error())
		return nil
	}
	m.setFlash(flash)
	m.buildRows()
	return m.probeAll()
}

// ---- settings ----

type settingsDraft struct {
	aptFull, aptAutoremove, aptAutoclean    bool
	nodeChannel                             string
	nodeReinstall                           bool
	stopOnFailure, checkOnStart             bool
	keepLogs, goRoot                        string
	cargoExclude, gupExclude, dockerExclude string
}

func joinList(l []string) string { return strings.Join(l, ", ") }

func splitList(s string) []string {
	out := []string{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		out = append(out, f)
	}
	return out
}

func toggle(title, desc string, v *bool) *huh.Confirm {
	return huh.NewConfirm().Title(title).Description(desc).Affirmative("On").Negative("Off").Value(v)
}

func (m *Model) openSettingsForm() tea.Cmd {
	c := m.cfg
	d := &settingsDraft{
		aptFull: c.Apt.FullUpgrade, aptAutoremove: c.Apt.Autoremove, aptAutoclean: c.Apt.Autoclean,
		nodeChannel: c.Node.Channel, nodeReinstall: c.Node.ReinstallPackages,
		stopOnFailure: c.General.StopOnFailure, checkOnStart: c.General.CheckOnStart,
		keepLogs: strconv.Itoa(c.General.KeepLogs), goRoot: c.Go.Root,
		cargoExclude: joinList(c.Cargo.Exclude), gupExclude: joinList(c.Gup.Exclude), dockerExclude: joinList(c.Docker.Exclude),
	}
	f := huh.NewForm(
		huh.NewGroup(
			toggle("full-upgrade", "Use apt-get full-upgrade, which may add or remove packages, instead of upgrade.", &d.aptFull),
			toggle("autoremove --purge", "Remove packages that are no longer needed.", &d.aptAutoremove),
			toggle("autoclean", "Delete obsolete .deb files from the package cache.", &d.aptAutoclean),
		).Title("Ubuntu packages (APT)"),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Node.js version to track").
				Options(
					huh.NewOption("Newest release of my current major", "current-major"),
					huh.NewOption("Latest LTS release", "lts"),
				).Value(&d.nodeChannel),
			toggle("Reinstall global npm packages", "Carry global packages over when a new Node version is installed.", &d.nodeReinstall),
			huh.NewInput().Title("Go install root").Description("Where the Go step installs Go.").
				Value(&d.goRoot).Validate(func(s string) error {
				if !strings.HasPrefix(strings.TrimSpace(s), "/") {
					return errors.New("must be an absolute path")
				}
				return nil
			}),
		).Title("Toolchains"),
		huh.NewGroup(
			huh.NewInput().Title("Cargo: crates never reinstalled").Description("Comma-separated crate names.").
				Placeholder("none").Value(&d.cargoExclude),
			huh.NewInput().Title("gup: Go binaries never updated").Description("Comma-separated binary names from $GOBIN.").
				Placeholder("none").Value(&d.gupExclude),
			huh.NewInput().Title("Docker: images never pulled").Description("Comma-separated, as repo:tag or just repo.").
				Placeholder("none").Value(&d.dockerExclude),
		).Title("Exclusions"),
		huh.NewGroup(
			toggle("Stop on first failure", "Skip the remaining steps after one fails.", &d.stopOnFailure),
			toggle("Check for updates on start", "Look for pending updates when the app opens. This uses the network.", &d.checkOnStart),
			huh.NewInput().Title("Run logs to keep").Value(&d.keepLogs).Validate(func(s string) error {
				if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 {
					return errors.New("enter a whole number of at least 1")
				}
				return nil
			}),
		).Title("General"),
	)
	return m.showForm("Settings", f, func(m *Model) tea.Cmd {
		c := m.cfg
		c.Apt.FullUpgrade, c.Apt.Autoremove, c.Apt.Autoclean = d.aptFull, d.aptAutoremove, d.aptAutoclean
		c.Node.Channel, c.Node.ReinstallPackages = d.nodeChannel, d.nodeReinstall
		c.Go.Root = strings.TrimRight(strings.TrimSpace(d.goRoot), "/")
		c.Cargo.Exclude, c.Gup.Exclude = splitList(d.cargoExclude), splitList(d.gupExclude)
		c.Docker.Exclude = splitList(d.dockerExclude)
		c.General.StopOnFailure, c.General.CheckOnStart = d.stopOnFailure, d.checkOnStart
		c.General.KeepLogs, _ = strconv.Atoi(strings.TrimSpace(d.keepLogs))
		return m.configChanged("settings saved")
	})
}

// ---- custom steps ----

func (m Model) updateCustom(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	km, list := customKeys, m.cfg.Custom
	switch {
	case key.Matches(k, km.Back):
		m.screen = scrOverview
	case key.Matches(k, km.Up):
		m.customCursor = max(0, m.customCursor-1)
	case key.Matches(k, km.Down):
		m.customCursor = max(0, min(len(list)-1, m.customCursor+1))
	case key.Matches(k, km.Add):
		return m, m.openCustomForm(-1)
	case key.Matches(k, km.Edit):
		if len(list) > 0 {
			return m, m.openCustomForm(m.customCursor)
		}
	case key.Matches(k, km.Delete):
		if len(list) > 0 {
			return m, m.confirmDeleteCustom(m.customCursor)
		}
	case key.Matches(k, km.MoveUp), key.Matches(k, km.MoveDown):
		i := m.customCursor
		j := i - 1
		if key.Matches(k, km.MoveDown) {
			j = i + 1
		}
		if len(list) > 1 && j >= 0 && j < len(list) {
			list[i], list[j] = list[j], list[i]
			m.customCursor = j
			return m, m.configChanged("order saved")
		}
	}
	return m, nil
}

func (m Model) viewCustom() string {
	var b strings.Builder
	b.WriteString(sTitle.Render("sysupdate") + "  " + sBold.Render("Custom steps") + "\n\n")
	b.WriteString(sMuted.Render(trunc("Your own shell commands. They run after the built-in steps, in this order.", m.width)) + "\n\n")
	if len(m.cfg.Custom) == 0 {
		b.WriteString(sMuted.Render("  No custom steps yet. Press a to add one.") + "\n")
	}
	for i, c := range m.cfg.Custom {
		cur := "  "
		if i == m.customCursor {
			cur = sAccent.Render("▸ ")
		}
		flags := ""
		if c.Sudo {
			flags += " " + sWarn.Render("sudo")
		}
		if c.Requires != "" {
			flags += sMuted.Render(" needs " + c.Requires)
		}
		line := cur + pad(sBold.Render(c.Name), 22) + " " + pad(sMuted.Render(c.ID), 16) + " " +
			sAccent.Render("$ ") + strings.ReplaceAll(c.Command, "\n", "; ") + flags
		line = trunc(line, m.width)
		if i == m.customCursor {
			line = sSel.Render(pad(line, m.width))
		}
		b.WriteString(line + "\n")
	}
	out := b.String()
	foot := m.footer(customKeys)
	if m.flash != "" {
		foot = sAccent.Render(trunc(m.flash, m.width)) + "\n" + foot
	}
	if gap := m.height - strings.Count(out, "\n") - strings.Count(foot, "\n") - 1; gap > 0 {
		out += strings.Repeat("\n", gap)
	}
	return out + foot
}

type customDraft struct {
	name, id, desc, command, requires, version string
	sudo                                       bool
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// openCustomForm adds a step (idx < 0) or edits cfg.Custom[idx].
func (m *Model) openCustomForm(idx int) tea.Cmd {
	d := &customDraft{}
	title := "Add custom step"
	if idx >= 0 {
		c := m.cfg.Custom[idx]
		d = &customDraft{c.Name, c.ID, c.Description, c.Command, c.Requires, c.Version, c.Sudo}
		title = "Edit custom step: " + c.Name
	}
	// IDs already taken by built-in steps and other custom steps.
	taken := map[string]bool{}
	for _, s := range steps.All(config.Default()) { // built-in steps only
		taken[s.ID] = true
	}
	for i, c := range m.cfg.Custom {
		if i != idx {
			taken[c.ID] = true
		}
	}
	required := func(what string) func(string) error {
		return func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New(what + " is required")
			}
			return nil
		}
	}
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Description("Shown in the step list.").Value(&d.name).Validate(required("a name")),
		huh.NewInput().Title("ID").Description("Used in the config and with --only/--skip. Leave empty to derive it from the name.").
			Value(&d.id).Validate(func(s string) error {
			id := strings.TrimSpace(s)
			if id == "" {
				id = slug(d.name)
			}
			switch {
			case id == "":
				return errors.New("enter an ID")
			case !idRE.MatchString(id):
				return errors.New("use lowercase letters, digits and dashes")
			case taken[id]:
				return fmt.Errorf("%q is already used by another step", id)
			}
			return nil
		}),
		huh.NewText().Title("Command").Description("Run with bash -c. Use several lines for several commands.").
			Lines(3).Value(&d.command).Validate(required("a command")),
		huh.NewConfirm().Title("Run with sudo").Affirmative("Yes").Negative("No").Value(&d.sudo),
		huh.NewInput().Title("Requires (optional)").Description("A program that must be on PATH. If missing, the step shows as not installed.").
			Placeholder("e.g. flatpak").Value(&d.requires),
		huh.NewInput().Title("Version command (optional)").Description("Its first output line is shown in the Version column.").
			Placeholder("e.g. flatpak --version").Value(&d.version),
		huh.NewInput().Title("Description (optional)").Value(&d.desc),
	))
	return m.showForm(title, f, func(m *Model) tea.Cmd {
		id := strings.TrimSpace(d.id)
		if id == "" {
			id = slug(d.name)
		}
		c := config.CustomStep{
			ID: id, Name: strings.TrimSpace(d.name), Description: strings.TrimSpace(d.desc),
			Command: strings.TrimSpace(d.command), Sudo: d.sudo,
			Requires: strings.TrimSpace(d.requires), Version: strings.TrimSpace(d.version),
		}
		if idx >= 0 {
			old := m.cfg.Custom[idx].ID
			if old != id { // keep the enabled state under the new ID
				if v, ok := m.cfg.Enabled[old]; ok {
					m.cfg.Enabled[id] = v
					delete(m.cfg.Enabled, old)
				}
			}
			m.cfg.Custom[idx] = c
			return m.configChanged("saved " + c.Name)
		}
		m.cfg.Custom = append(m.cfg.Custom, c)
		m.customCursor = len(m.cfg.Custom) - 1
		return m.configChanged("added " + c.Name)
	})
}

func (m *Model) confirmDeleteCustom(idx int) tea.Cmd {
	c := m.cfg.Custom[idx]
	var yes bool
	f := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(fmt.Sprintf("Delete %q?", c.Name)).
			Description("$ " + c.Command).Affirmative("Delete").Negative("Keep").Value(&yes),
	))
	return m.showForm("Delete custom step", f, func(m *Model) tea.Cmd {
		if !yes {
			return nil
		}
		m.cfg.Custom = append(m.cfg.Custom[:idx], m.cfg.Custom[idx+1:]...)
		delete(m.cfg.Enabled, c.ID)
		m.customCursor = max(0, min(m.customCursor, len(m.cfg.Custom)-1))
		return m.configChanged("deleted " + c.Name)
	})
}
