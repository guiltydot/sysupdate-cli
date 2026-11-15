// Command sysupdate updates the whole WSL/Ubuntu dev environment, with a Bubble Tea TUI.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/guiltydot/sysupdate-cli"
	"github.com/guiltydot/sysupdate-cli/internal/config"
	"github.com/guiltydot/sysupdate-cli/internal/runner"
	"github.com/guiltydot/sysupdate-cli/internal/state"
	"github.com/guiltydot/sysupdate-cli/internal/steps"
	"github.com/guiltydot/sysupdate-cli/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
// For `go install module@version` builds it falls back to the module version.
var version = ""

func init() {
	if version != "" {
		return
	}
	version = "dev"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
}

const usage = `sysupdate — update the dev environment (APT, Go, gup, Rust, Cargo, Node, pnpm, uv, pyenv, Claude Code, custom steps)

Usage:
  sysupdate                     open the interactive TUI
  sysupdate setup               choose which steps to use (shown automatically on first run)
  sysupdate run [flags]         run enabled steps without the TUI (plain output)
      --only a,b                run just these step IDs
      --skip a,b                skip these step IDs
      --all                     run every installed step, ignoring the config
      --dry-run                 print the commands instead of running them
  sysupdate check               show which steps have updates available
  sysupdate clean [flags]       remove leftovers the checks found (asks first)
      --only a,b                just these step IDs
      --yes                     do not ask
      --dry-run                 only list them
  sysupdate list                list steps, IDs, enabled state and versions
  sysupdate config [path|init|edit]
                                show the config, print its path, write defaults, or open $EDITOR
  sysupdate logs [-n N]         print the most recent run log (or the N-th newest)
  sysupdate help [--full]       this summary, or the full manual (also ? in the TUI)
  sysupdate version

Exit status of run: 0 all OK, 1 a step failed, 2 usage/config error, 130 cancelled.
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	cfg, cfgErr := config.Load()

	args := flag.Args()
	if len(args) == 0 {
		if cfgErr != nil && !term.IsTerminal(os.Stdout.Fd()) {
			fatal(cfgErr)
		}
		// No config file yet: this is a new machine, so start with setup.
		if err := tui.Run(cfg, cfgErr, sysupdate.Help, cfgErr == nil && !config.Exists()); err != nil {
			fatal(err)
		}
		return
	}
	if cfgErr != nil && args[0] != "config" {
		fatal(cfgErr)
	}
	switch args[0] {
	case "run":
		os.Exit(cmdRun(cfg, args[1:]))
	case "setup":
		if !term.IsTerminal(os.Stdout.Fd()) {
			fatal(fmt.Errorf("setup needs a terminal"))
		}
		if err := tui.Run(cfg, cfgErr, sysupdate.Help, true); err != nil {
			fatal(err)
		}
	case "clean":
		os.Exit(cmdClean(cfg, args[1:]))
	case "check":
		cmdCheck(cfg)
	case "list", "ls":
		cmdList(cfg)
	case "config":
		cmdConfig(cfg, args[1:])
	case "logs", "log":
		cmdLogs(args[1:])
	case "version", "--version":
		fmt.Println("sysupdate", version)
	case "help", "-h", "--help":
		if len(args) > 1 && args[1] == "--full" {
			fmt.Print(sysupdate.Help)
			return
		}
		flag.Usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		flag.Usage()
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sysupdate:", err)
	os.Exit(2)
}

func splitIDs(s string) map[string]bool {
	m := map[string]bool{}
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			m[id] = true
		}
	}
	return m
}

func cmdRun(cfg *config.Config, args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	only := fs.String("only", "", "comma-separated step IDs to run")
	skip := fs.String("skip", "", "comma-separated step IDs to skip")
	all := fs.Bool("all", false, "run every installed step")
	dry := fs.Bool("dry-run", false, "print commands without running them")
	_ = fs.Parse(args)

	onlySet, skipSet := splitIDs(*only), splitIDs(*skip)
	known := map[string]bool{}
	var list []*steps.Step
	for _, s := range steps.All(cfg) {
		known[s.ID] = true
		switch {
		case len(onlySet) > 0 && !onlySet[s.ID], skipSet[s.ID]:
			continue
		case len(onlySet) == 0 && !*all && !cfg.IsEnabled(s.ID, s.DefaultOn):
			continue
		}
		list = append(list, s)
	}
	for id := range onlySet {
		if !known[id] {
			fmt.Fprintf(os.Stderr, "unknown step %q (see `sysupdate list`)\n", id)
			return 2
		}
	}
	if len(list) == 0 {
		fmt.Println("Nothing to run.")
		return 0
	}

	if !config.Exists() && len(onlySet) == 0 && !*all {
		fmt.Fprintln(os.Stderr, stMuted.Render("No config yet, so running the default steps that are installed. Run `sysupdate setup` to choose."))
	}
	ctx := context.Background()
	if *dry {
		for _, s := range list {
			fmt.Printf("== %s [%s]\n", s.Name, s.ID)
			if !s.Available() {
				fmt.Println("   (not installed, skipped)")
				continue
			}
			acts, err := s.Plan(ctx, cfg)
			if err != nil {
				fmt.Println("   cannot plan:", err)
			}
			for _, a := range acts {
				fmt.Println("   $", a.Display)
			}
		}
		return 0
	}
	return runAndReport(ctx, cfg, list)
}

// runAndReport asks for sudo if needed, runs list with streamed output and prints
// a summary table. It returns the exit status.
func runAndReport(ctx context.Context, cfg *config.Config, list []*steps.Step) int {
	if runner.NeedsSudo(list) && !runner.SudoCached() {
		cmd := exec.Command("sudo", "-v")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "sudo is required for:", stepsNeedingSudo(list))
			return 2
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	color := isTTY()
	// Lines may be replaced by the next one (\r progress output). On a terminal we
	// redraw in place; otherwise we hold the last line and print only its final state.
	pending, has := "", false
	flush := func() {
		if has {
			if !color {
				fmt.Println(pending)
			} else {
				fmt.Println()
			}
			has = false
		}
	}
	style := func(text string) string {
		switch {
		case strings.HasPrefix(text, "$ "):
			return stCmd.Render(text)
		case strings.HasPrefix(text, "!! "):
			return stErr.Render(text)
		}
		return text
	}
	fmt.Println(stTitle.Render("sysupdate") + stMuted.Render(fmt.Sprintf("  running %d step(s)", len(list))))
	done := runner.Run(ctx, cfg, list, func(ev any) {
		switch e := ev.(type) {
		case runner.StepStart:
			flush()
			fmt.Println("\n" + stStep.Render("▸ "+list[e.Idx].Name))
		case runner.Line:
			if color {
				if e.Replace && has {
					fmt.Print("\r\x1b[2K" + style(e.Text)) // clear the line and redraw
				} else {
					flush()
					fmt.Print(style(e.Text))
				}
				has = true
				return
			}
			if !e.Replace {
				flush()
			}
			pending, has = e.Text, true
		case runner.StepDone:
			flush()
			if r := e.Result; r.Status == runner.Skipped || r.Status == runner.Cancelled {
				fmt.Println(stMuted.Render("↷ " + r.Name + ": " + r.Err))
			}
		}
	})
	flush()

	fmt.Println()
	failed := 0
	var rows [][]string
	for _, r := range done.Results {
		if r.Status == runner.Failed {
			failed++
		}
		rows = append(rows, []string{r.Name, statusLabel(r.Status), r.Duration.Round(100 * time.Millisecond).String(), r.Err})
	}
	printTable([]string{"Step", "Result", "Time", "Notes"}, rows, func(row, col int) *lipgloss.Style {
		if col != 1 {
			return nil
		}
		s := statusStyle(done.Results[row].Status)
		return &s
	})
	if done.LogPath != "" {
		fmt.Println(stMuted.Render("Log: " + done.LogPath))
	}
	if done.RebootRequired {
		fmt.Println(stWarn.Render("⚠ A restart is required: run 'wsl --shutdown' from Windows, then reopen the shell."))
	}
	switch {
	case done.Cancelled:
		return 130
	case failed > 0:
		return 1
	}
	return 0
}

func statusLabel(s runner.Status) string {
	return map[runner.Status]string{runner.OK: "✓ ok", runner.Failed: "✗ failed", runner.Skipped: "↷ skipped", runner.Cancelled: "■ cancelled"}[s]
}

func statusStyle(s runner.Status) lipgloss.Style {
	switch s {
	case runner.OK:
		return stOK
	case runner.Failed:
		return stErr
	case runner.Cancelled:
		return stWarn
	}
	return stMuted
}

func stepsNeedingSudo(list []*steps.Step) string {
	var n []string
	for _, s := range list {
		if s.NeedsSudo {
			n = append(n, s.Name)
		}
	}
	return strings.Join(n, ", ")
}

// runChecks runs the checks of all steps in parallel.
func runChecks(cfg *config.Config, all []*steps.Step) []steps.CheckResult {
	res := make([]steps.CheckResult, len(all))
	var wg sync.WaitGroup
	for i, s := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch {
			case !s.Available():
				res[i] = steps.CheckResult{Summary: "not installed"}
			case s.Check == nil:
				res[i] = steps.CheckResult{Summary: "no check available"}
			default:
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				res[i] = s.Check(ctx, cfg)
			}
		}()
	}
	wg.Wait()
	return res
}

func cmdCheck(cfg *config.Config) {
	all := steps.All(cfg)
	res := runChecks(cfg, all)
	var rows [][]string
	outdated := 0
	var cleanup []string
	for i, s := range all {
		r := res[i]
		mark := map[steps.CheckState]string{steps.CheckUnknown: "–", steps.CheckUpToDate: "✓", steps.CheckOutdated: "↑", steps.CheckError: "✗"}[r.State]
		detail := r.Summary
		if len(r.Details) > 0 {
			detail += "\n" + stMuted.Render("• "+strings.Join(r.Details, "\n• "))
		}
		for _, c := range r.Cleanup {
			cleanup = append(cleanup, s.Name+": "+c.Text+"  "+stMuted.Render("$ "+c.Action.Display))
		}
		if r.State == steps.CheckOutdated {
			outdated++
		}
		rows = append(rows, []string{s.Name, mark, detail})
	}
	printTable([]string{"Step", "", "Updates"}, rows, func(row, col int) *lipgloss.Style {
		if col == 0 {
			return nil
		}
		var st lipgloss.Style
		switch res[row].State {
		case steps.CheckUpToDate:
			st = stOK
		case steps.CheckOutdated:
			st = stWarn
		case steps.CheckError:
			st = stErr
		default:
			st = stMuted
		}
		return &st
	})
	if outdated > 0 {
		fmt.Println(stWarn.Render(fmt.Sprintf("%d step(s) have updates. Run `sysupdate` to install them.", outdated)))
	}
	if len(cleanup) > 0 {
		fmt.Println("\n" + stStep.Render("Could be removed") + stMuted.Render(" (remove them with `sysupdate clean`)"))
		for _, c := range cleanup {
			fmt.Println(stWarn.Render("• ") + c)
		}
	}
}

// cmdClean removes the leftovers the checks found, after asking.
func cmdClean(cfg *config.Config, args []string) int {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	only := fs.String("only", "", "comma-separated step IDs to clean up")
	yes := fs.Bool("yes", false, "do not ask before removing")
	dry := fs.Bool("dry-run", false, "only list what would be removed")
	_ = fs.Parse(args)
	onlySet := splitIDs(*only)

	var all []*steps.Step
	for _, s := range steps.All(cfg) {
		if (len(onlySet) == 0 || onlySet[s.ID]) && s.Available() {
			all = append(all, s)
		}
	}
	fmt.Println(stMuted.Render("Looking for leftovers…"))
	res := runChecks(cfg, all)
	var list []*steps.Step
	for i, s := range all {
		if items := res[i].Cleanup; len(items) > 0 {
			fmt.Println("\n" + stStep.Render("▸ "+s.Name))
			for _, c := range items {
				fmt.Println(stWarn.Render("• ") + c.Text)
				fmt.Println(stCmd.Render("  $ " + c.Action.Display))
			}
			list = append(list, steps.CleanupStep(s, items))
		}
	}
	if len(list) == 0 {
		fmt.Println(stOK.Render("Nothing to clean up."))
		return 0
	}
	if *dry {
		return 0
	}
	if !*yes {
		if !isTTY() {
			fmt.Fprintln(os.Stderr, "\nnot a terminal: pass --yes to remove these")
			return 2
		}
		fmt.Print("\nRemove all of these? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Nothing removed.")
			return 0
		}
	}
	return runAndReport(context.Background(), cfg, list)
}

func cmdList(cfg *config.Config) {
	st := state.Load()
	ctx := context.Background()
	all := steps.All(cfg)
	avail := make([]bool, len(all))
	var rows [][]string
	yesNo := func(b bool) string {
		if b {
			return "✓"
		}
		return "–"
	}
	for i, s := range all {
		avail[i] = s.Available()
		ver := ""
		if avail[i] {
			ver = s.Version(ctx, cfg)
		}
		last := "never"
		if r, ok := st.Steps[s.ID]; ok {
			last = r.LastRun.Format("2006-01-02 15:04")
			if !r.OK {
				last += " ✗"
			}
		}
		rows = append(rows, []string{s.ID, s.Name, yesNo(cfg.IsEnabled(s.ID, s.DefaultOn)), yesNo(avail[i]), ver, last})
	}
	printTable([]string{"ID", "Name", "Enabled", "Installed", "Version", "Last run"}, rows, func(row, col int) *lipgloss.Style {
		switch {
		case col == 0:
			s := stCmd
			return &s
		case !avail[row]:
			s := stMuted
			return &s
		case (col == 2 || col == 3) && rows[row][col] == "✓":
			s := stOK
			return &s
		case col == 5 && strings.HasSuffix(rows[row][col], "✗"):
			s := stErr
			return &s
		}
		return nil
	})
}

func cmdConfig(cfg *config.Config, args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "path":
		fmt.Println(config.Path())
	case "init":
		if _, err := os.Stat(config.Path()); err == nil {
			fmt.Println("config already exists:", config.Path())
			return
		}
		if err := config.Default().Save(); err != nil {
			fatal(err)
		}
		fmt.Println("wrote", config.Path())
	case "edit":
		if _, err := os.Stat(config.Path()); err != nil {
			if err := cfg.Save(); err != nil {
				fatal(err)
			}
		}
		ed := os.Getenv("VISUAL")
		if ed == "" {
			ed = os.Getenv("EDITOR")
		}
		if ed == "" {
			ed = "nano"
		}
		c := exec.Command("sh", "-c", ed+` ""`, "sh", config.Path()) //nolint:gosec //  may hold arguments, run through sh like git does; the path is passed as
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			fatal(err)
		}
		if _, err := config.Load(); err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	case "":
		b, err := os.ReadFile(config.Path())
		if err != nil {
			fmt.Println("# no config file yet; defaults in use (sysupdate config init to write them)")
			fmt.Println("# path:", config.Path())
			return
		}
		_, _ = os.Stdout.Write(b)
	default:
		fatal(fmt.Errorf("unknown config subcommand %q", sub))
	}
}

func cmdLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	n := fs.Int("n", 1, "which log, 1 = newest")
	list := fs.Bool("list", false, "list log files")
	_ = fs.Parse(args)
	logs := runner.Logs()
	if *list {
		for _, l := range logs {
			fmt.Println(l)
		}
		return
	}
	if *n < 1 || *n > len(logs) {
		fmt.Println("no such log")
		return
	}
	fmt.Println(runner.ReadLog(logs[*n-1]))
}
