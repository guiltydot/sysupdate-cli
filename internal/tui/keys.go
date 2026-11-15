package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

func bind(keys []string, k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
}

// Shared bindings.
var (
	keyHelp   = bind([]string{"?", "f1"}, "?", "help")
	keyUp     = bind([]string{"up", "k"}, "↑/k", "up")
	keyDown   = bind([]string{"down", "j"}, "↓/j", "down")
	keyTop    = bind([]string{"home", "g"}, "g", "top")
	keyBottom = bind([]string{"end", "G"}, "G", "bottom")
	keyScroll = bind([]string{"pgup", "pgdown", "ctrl+u", "ctrl+d", "b", " "}, "pgup/pgdn", "scroll")
	keyQuit   = bind([]string{"q"}, "q", "quit")
	keyForce  = bind([]string{"ctrl+c"}, "ctrl+c", "quit")
)

// ---- overview ----

type overviewKeyMap struct {
	Up, Down, Top, Bottom, Toggle, All, Outdated, Run, RunOne, Check, CheckOne,
	Clean, Settings, Custom, Edit, Logs, Help, Quit key.Binding
}

var overviewKeys = overviewKeyMap{
	Up: keyUp, Down: keyDown, Top: keyTop, Bottom: keyBottom,
	Toggle:   bind([]string{" ", "x"}, "space", "toggle"),
	All:      bind([]string{"a"}, "a", "select all/none"),
	Outdated: bind([]string{"u"}, "u", "select outdated"),
	Run:      bind([]string{"enter", "r"}, "enter", "run"),
	RunOne:   bind([]string{"o"}, "o", "run this"),
	Check:    bind([]string{"c"}, "c", "check"),
	CheckOne: bind([]string{"C"}, "C", "check this"),
	Clean:    bind([]string{"d"}, "d", "clean up"),
	Settings: bind([]string{"s"}, "s", "settings"),
	Custom:   bind([]string{"m"}, "m", "custom steps"),
	Edit:     bind([]string{"e"}, "e", "edit config"),
	Logs:     bind([]string{"l"}, "l", "logs"),
	Help:     keyHelp,
	Quit:     bind([]string{"q", "esc"}, "q", "quit"),
}

func (k overviewKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.All, k.Outdated, k.Run, k.RunOne, k.Check, k.Clean, k.Settings, k.Custom, k.Logs, k.Help, k.Quit}
}

func (k overviewKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Toggle, k.All, k.Outdated},
		{k.Run, k.RunOne, k.Check, k.CheckOne, k.Clean},
		{k.Settings, k.Custom, k.Edit, k.Logs, k.Help, k.Quit},
	}
}

// ---- run ----

type runKeyMap struct {
	Up, Down, Follow, Scroll, Top, Bottom, Cancel, Retry, Logs, Back, Help, Quit key.Binding
}

func newRunKeys(finished bool) runKeyMap {
	k := runKeyMap{
		Up:     bind([]string{"up", "k"}, "↑/↓", "step"),
		Down:   bind([]string{"down", "j"}, "↓", "step"),
		Follow: bind([]string{"f"}, "f", "follow"),
		Scroll: keyScroll,
		Top:    bind([]string{"home"}, "home", "top"),
		Bottom: bind([]string{"end"}, "end", "bottom"),
		Cancel: bind([]string{"X", "ctrl+c"}, "X/ctrl+c", "cancel"),
		Retry:  bind([]string{"r"}, "r", "retry failed"),
		Logs:   bind([]string{"l"}, "l", "logs"),
		Back:   bind([]string{"enter", "esc"}, "enter", "back"),
		Help:   keyHelp,
		Quit:   keyQuit,
	}
	k.Follow.SetEnabled(!finished)
	k.Cancel.SetEnabled(!finished)
	for _, b := range []*key.Binding{&k.Retry, &k.Logs, &k.Back, &k.Quit} {
		b.SetEnabled(finished)
	}
	return k
}

func (k runKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Follow, k.Scroll, k.Cancel, k.Retry, k.Logs, k.Help, k.Back, k.Quit}
}

func (k runKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// ---- logs / help screen ----

type pagerKeyMap struct {
	Scroll, Older, Newer, Top, Bottom, Back, Help key.Binding
}

var logKeys = pagerKeyMap{
	Scroll: bind([]string{"pgup", "pgdown", "up", "down", "k", "j", "ctrl+u", "ctrl+d", " ", "b"}, "↑/↓/pgup/pgdn", "scroll"),
	Older:  bind([]string{"left", "h", "["}, "←", "older"),
	Newer:  bind([]string{"right", "l", "]"}, "→", "newer"),
	Top:    keyTop, Bottom: keyBottom,
	Back: bind([]string{"esc", "q"}, "esc", "back"),
	Help: keyHelp,
}

var helpKeys = func() pagerKeyMap {
	k := logKeys
	k.Older.SetEnabled(false)
	k.Newer.SetEnabled(false)
	k.Back = bind([]string{"esc", "q", "?", "f1", "enter"}, "esc/?", "close")
	k.Help.SetEnabled(false)
	return k
}()

func (k pagerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Scroll, k.Older, k.Newer, k.Top, k.Bottom, k.Help, k.Back}
}

func (k pagerKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// ---- settings ----

type settingsKeyMap struct {
	Up, Down, Top, Bottom, Change, Left, Right, Back, Help key.Binding
}

var settingsKeys = settingsKeyMap{
	Up:     bind([]string{"up", "k", "shift+tab"}, "↑/↓", "move"),
	Down:   bind([]string{"down", "j", "tab"}, "↓", "down"),
	Top:    keyTop,
	Bottom: keyBottom,
	Change: bind([]string{" ", "enter"}, "space/enter", "change"),
	Left:   bind([]string{"left", "h"}, "←", "previous"),
	Right:  bind([]string{"right", "l"}, "→", "next"),
	Back:   bind([]string{"esc", "q", "s"}, "esc", "back"),
	Help:   keyHelp,
}

func (k settingsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Change, k.Help, k.Back}
}

func (k settingsKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// ---- first-run setup ----

type setupKeyMap struct {
	Up, Down, Toggle, All, Save, Skip, Help, Quit key.Binding
}

var setupKeys = setupKeyMap{
	Up: keyUp, Down: keyDown,
	Toggle: bind([]string{" ", "x"}, "space", "toggle"),
	All:    bind([]string{"a"}, "a", "all/none"),
	Save:   bind([]string{"enter"}, "enter", "save and continue"),
	Skip:   bind([]string{"esc"}, "esc", "skip (keep defaults)"),
	Help:   keyHelp,
	Quit:   bind([]string{"q"}, "q", "quit without saving"),
}

func (k setupKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Toggle, k.All, k.Save, k.Skip, k.Help, k.Quit}
}

func (k setupKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// ---- custom steps list ----

type customKeyMap struct {
	Up, Down, Add, Edit, Delete, MoveUp, MoveDown, Back, Help key.Binding
}

var customKeys = customKeyMap{
	Up: keyUp, Down: keyDown,
	Add:      bind([]string{"a", "n"}, "a", "add"),
	Edit:     bind([]string{"enter", "e"}, "enter", "edit"),
	Delete:   bind([]string{"d", "delete"}, "d", "delete"),
	MoveUp:   bind([]string{"K", "shift+up"}, "K", "move up"),
	MoveDown: bind([]string{"J", "shift+down"}, "J", "move down"),
	Back:     bind([]string{"esc", "q"}, "esc", "back"),
	Help:     keyHelp,
}

func (k customKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Edit, k.Delete, k.MoveUp, k.MoveDown, k.Help, k.Back}
}

func (k customKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// newHelp returns a help.Model styled to match the rest of the UI.
func newHelp() help.Model {
	h := help.New()
	h.ShortSeparator = " · "
	h.Styles.ShortKey = sKey
	h.Styles.ShortDesc = sMuted
	h.Styles.ShortSeparator = sMuted
	h.Styles.FullKey = sKey
	h.Styles.FullDesc = sMuted
	h.Styles.FullSeparator = sMuted
	h.Styles.Ellipsis = sMuted
	return h
}
