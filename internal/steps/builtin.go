package steps

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

func home() string { h, _ := os.UserHomeDir(); return h }

// ---------- APT ----------

func aptStep() *Step {
	return &Step{
		ID: "apt", Name: "Ubuntu packages", NeedsSudo: true, DefaultOn: true,
		Description: "APT package lists, upgrades and cleanup (non-interactive, keeps existing config files).",
		Available:   func() bool { return Have("apt-get") },
		Version: func(ctx context.Context, cfg *config.Config) string {
			data, err := os.ReadFile("/etc/os-release")
			if err != nil {
				return ""
			}
			for _, l := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
					return strings.Trim(v, `"`)
				}
			}
			return ""
		},
		Check: func(ctx context.Context, cfg *config.Config) CheckResult {
			r := aptCheck(ctx, cfg)
			r.Cleanup = aptCleanup(ctx)
			return r
		},
		Plan: aptPlan,
	}
}

// aptCheck lists upgradable packages from the cached package lists,
// so it reflects the last `apt update`.
func aptCheck(ctx context.Context, cfg *config.Config) CheckResult {
	upgrade := "upgrade"
	if cfg.Apt.FullUpgrade {
		upgrade = "full-upgrade"
	}
	// A simulated upgrade shows what the APT step would really install. Unlike
	// `apt list --upgradable`, it leaves out phased updates, which Ubuntu rolls
	// out gradually and apt holds back until this machine's turn.
	out, err := Output(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", upgrade)
	if err != nil {
		return errResult(err)
	}
	pkgs, phased, kept := parseAptSim(out)
	var notes []string
	if len(phased) > 0 {
		notes = append(notes, fmt.Sprintf("%s held back by phased rollout: %s", plural(len(phased), "package"), strings.Join(phased, " ")))
	}
	if len(kept) > 0 {
		notes = append(notes, fmt.Sprintf("%s kept back (turn on full-upgrade to install): %s", plural(len(kept), "package"), strings.Join(kept, " ")))
	}
	if len(pkgs) == 0 {
		summary := "up to date"
		if len(phased)+len(kept) > 0 {
			summary += fmt.Sprintf(" (%d held back)", len(phased)+len(kept))
		}
		return CheckResult{State: CheckUpToDate, Summary: summary, Details: notes}
	}
	return outdated(plural(len(pkgs), "package"), append(pkgs, notes...)...)
}

// parseAptSim reads `apt-get -s upgrade` output: packages it would install
// ("Inst name [old] (new ...)"), and those deferred by phasing or kept back.
func parseAptSim(out string) (pkgs, phased, kept []string) {
	var list *[]string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "Inst "):
			f := strings.Fields(l)
			name, from, to := f[1], "", ""
			for _, x := range f[2:] {
				switch {
				case strings.HasPrefix(x, "[") && from == "" && to == "":
					from = strings.Trim(x, "[]")
				case strings.HasPrefix(x, "(") && to == "":
					to = strings.TrimPrefix(x, "(")
				}
			}
			if from != "" {
				pkgs = append(pkgs, fmt.Sprintf("%s %s → %s", name, from, to))
			} else {
				pkgs = append(pkgs, fmt.Sprintf("%s %s (new)", name, to))
			}
			list = nil
		case strings.Contains(l, "deferred due to phasing"):
			list = &phased
		case strings.Contains(l, "have been kept back"):
			list = &kept
		case strings.HasPrefix(l, "  ") && list != nil:
			*list = append(*list, strings.Fields(l)...)
		default:
			list = nil
		}
	}
	return
}

// aptCleanup reports packages that autoremove would remove and packages
// removed earlier whose config files are still on disk. Nothing is deleted.
func aptCleanup(ctx context.Context) []Cleanup {
	var out []Cleanup
	env := []string{"DEBIAN_FRONTEND=noninteractive"}
	if sim, err := Output(ctx, "apt-get", "-s", "autoremove"); err == nil {
		var names []string
		for _, l := range strings.Split(sim, "\n") {
			if rest, ok := strings.CutPrefix(l, "Remv "); ok {
				names = append(names, strings.Fields(rest)[0])
			}
		}
		if len(names) > 0 {
			out = append(out, Cleanup{
				Text:   fmt.Sprintf("%s no longer needed: %s", plural(len(names), "package"), strings.Join(names, " ")),
				Sudo:   true,
				Action: Exec("apt autoremove", true, env, "apt-get", "-y", "autoremove", "--purge"),
			})
		}
	}
	if list, err := Output(ctx, "dpkg-query", "-W", "-f", "${db:Status-Abbrev} ${Package}\n"); err == nil {
		var names []string
		for _, l := range strings.Split(list, "\n") {
			if f := strings.Fields(l); len(f) == 2 && f[0] == "rc" {
				names = append(names, f[1])
			}
		}
		if len(names) > 0 {
			out = append(out, Cleanup{
				Text:   fmt.Sprintf("%s removed but config files left: %s", plural(len(names), "package"), strings.Join(names, " ")),
				Sudo:   true,
				Action: Exec("apt purge", true, env, "apt-get", append([]string{"-y", "purge"}, names...)...),
			})
		}
	}
	return out
}

func aptPlan(ctx context.Context, cfg *config.Config) ([]Action, error) {
	env := []string{"DEBIAN_FRONTEND=noninteractive"}
	dpkg := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	upgrade := "upgrade"
	if cfg.Apt.FullUpgrade {
		upgrade = "full-upgrade"
	}
	acts := []Action{
		Exec("apt update", true, env, "apt-get", "update"),
		Exec("apt "+upgrade, true, env, "apt-get", append([]string{"-y", upgrade}, dpkg...)...),
	}
	if cfg.Apt.Autoremove {
		acts = append(acts, Exec("apt autoremove", true, env, "apt-get", "-y", "autoremove", "--purge"))
	}
	if cfg.Apt.Autoclean {
		acts = append(acts, Exec("apt autoclean", true, env, "apt-get", "-y", "autoclean"))
	}
	return acts, nil
}

// ---------- Rust ----------

func rustupStep() *Step {
	return &Step{
		ID: "rustup", Name: "Rust toolchain", DefaultOn: true,
		Description: "rustup update: installed toolchains and rustup itself.",
		Available:   func() bool { return Have("rustup") },
		Version: func(ctx context.Context, cfg *config.Config) string {
			out, _ := Output(ctx, "rustc", "--version")
			return strings.TrimPrefix(firstLine(out), "rustc ")
		},
		Check: func(ctx context.Context, _ *config.Config) CheckResult {
			out, err := CombinedOutput(ctx, "rustup", "check")
			if err != nil && out == "" {
				return errResult(err)
			}
			var ups []string
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, "Update available") {
					ups = append(ups, strings.TrimSpace(l))
				}
			}
			if len(ups) == 0 {
				return upToDate()
			}
			return outdated(plural(len(ups), "update"), ups...)
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			return []Action{Exec("rustup update", false, nil, "rustup", "update")}, nil
		},
	}
}

// ---------- Cargo ----------

type crate struct{ name, version string }

// installedCrates parses `cargo install --list`, skipping git/path installs
// (a plain `cargo install` would replace those with the crates.io version).
func installedCrates(ctx context.Context) ([]crate, []string, error) {
	out, err := Output(ctx, "cargo", "install", "--list")
	if err != nil {
		return nil, nil, err
	}
	var list []crate
	var skipped []string
	for _, l := range strings.Split(out, "\n") {
		if l == "" || strings.HasPrefix(l, " ") {
			continue
		}
		l = strings.TrimSuffix(l, ":")
		fields := strings.Fields(l)
		if len(fields) < 2 {
			continue
		}
		if len(fields) > 2 { // "name v1.0.0 (https://github.com/...)"
			skipped = append(skipped, fields[0])
			continue
		}
		list = append(list, crate{fields[0], strings.TrimPrefix(fields[1], "v")})
	}
	return list, skipped, nil
}

// latestCrate reads the crates.io sparse index for the newest non-yanked release.
func latestCrate(ctx context.Context, name string) (string, error) {
	n := strings.ToLower(name)
	var path string
	switch len(n) {
	case 1:
		path = "1/" + n
	case 2:
		path = "2/" + n
	case 3:
		path = "3/" + n[:1] + "/" + n
	default:
		path = n[:2] + "/" + n[2:4] + "/" + n
	}
	body, err := getText(ctx, "https://index.crates.io/"+path)
	if err != nil {
		return "", err
	}
	best := ""
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	re := regexp.MustCompile(`"vers":"([^"]+)".*"yanked":(true|false)`)
	for sc.Scan() {
		m := re.FindStringSubmatch(sc.Text())
		if m == nil || m[2] == "true" || isPrerelease(m[1]) {
			continue
		}
		if best == "" || compareVersions(m[1], best) > 0 {
			best = m[1]
		}
	}
	if best == "" {
		return "", fmt.Errorf("no releases of %s", name)
	}
	return best, nil
}

func cargoStep() *Step {
	return &Step{
		ID: "cargo", Name: "Cargo binaries", DefaultOn: true,
		Description: "Reinstalls outdated `cargo install` crates from crates.io (git/path installs are skipped).",
		Available:   func() bool { return Have("cargo") },
		Version: func(ctx context.Context, cfg *config.Config) string {
			list, _, err := installedCrates(ctx)
			if err != nil {
				return ""
			}
			return plural(len(list), "crate")
		},
		Check: func(ctx context.Context, cfg *config.Config) CheckResult {
			list, _, err := installedCrates(ctx)
			if err != nil {
				return errResult(err)
			}
			type res struct {
				c      crate
				latest string
				err    error
			}
			ch := make(chan res, len(list))
			for _, c := range list {
				go func() {
					v, err := latestCrate(ctx, c.name)
					ch <- res{c, v, err}
				}()
			}
			var ups, errs []string
			for range list {
				r := <-ch
				switch {
				case r.err != nil:
					errs = append(errs, r.c.name+": "+r.err.Error())
				case compareVersions(r.latest, r.c.version) > 0:
					mark := ""
					if contains(cfg.Cargo.Exclude, r.c.name) {
						mark = " (excluded)"
					}
					ups = append(ups, fmt.Sprintf("%s %s → %s%s", r.c.name, r.c.version, r.latest, mark))
				}
			}
			if len(ups) == 0 && len(errs) > 0 {
				return CheckResult{State: CheckError, Summary: "check failed", Details: errs}
			}
			if len(ups) == 0 {
				return upToDate()
			}
			return outdated(plural(len(ups), "crate"), append(ups, errs...)...)
		},
		Plan: func(ctx context.Context, cfg *config.Config) ([]Action, error) {
			list, _, err := installedCrates(ctx)
			if err != nil {
				return nil, err
			}
			var acts []Action
			for _, c := range list {
				if contains(cfg.Cargo.Exclude, c.name) {
					continue
				}
				// cargo only rebuilds when a newer version exists.
				acts = append(acts, Exec("cargo "+c.name, false, nil, "cargo", "install", "--locked", c.name))
			}
			return acts, nil
		},
	}
}

// ---------- Node via nvm ----------

func nvmDir() string {
	if d := os.Getenv("NVM_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".nvm")
}

const nvmPrelude = `export NVM_DIR="${NVM_DIR:-$HOME/.nvm}"; . "$NVM_DIR/nvm.sh" || exit 1; `

func nvmScript(script string) string { return nvmPrelude + script }

func nodeMajor(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	major, _, _ := strings.Cut(v, ".")
	return major
}

type nodeRelease struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"` // false or codename
}

func nodeStep() *Step {
	return &Step{
		ID: "node", Name: "Node.js (nvm)", DefaultOn: true,
		Description: "Installs the newest Node.js for the current major (or latest LTS) and makes it the nvm default.",
		Available:   func() bool { _, err := os.Stat(filepath.Join(nvmDir(), "nvm.sh")); return err == nil },
		Version: func(ctx context.Context, cfg *config.Config) string {
			out, _ := Output(ctx, "node", "--version")
			return out
		},
		Check: withCleanup(func(ctx context.Context, cfg *config.Config) CheckResult {
			cur, err := Output(ctx, "node", "--version")
			if err != nil {
				return errResult(fmt.Errorf("node not on PATH: %w", err))
			}
			var rels []nodeRelease
			if err := GetJSON(ctx, "https://nodejs.org/dist/index.json", &rels); err != nil {
				return errResult(err)
			}
			want := ""
			for _, r := range rels { // newest first
				if cfg.Node.Channel == "lts" {
					if lts, ok := r.LTS.(string); ok && lts != "" {
						want = r.Version
						break
					}
				} else if nodeMajor(r.Version) == nodeMajor(cur) {
					want = r.Version
					break
				}
			}
			if want == "" || compareVersions(want, cur) <= 0 {
				return upToDate()
			}
			return outdated(cur + " → " + want)
		}, nodeCleanup),
		Plan: func(ctx context.Context, cfg *config.Config) ([]Action, error) {
			target := "lts/*"
			if cfg.Node.Channel != "lts" {
				cur, err := Output(ctx, "node", "--version")
				if err != nil || nodeMajor(cur) == "" {
					return nil, fmt.Errorf("cannot determine current Node major (is node on PATH?)")
				}
				target = nodeMajor(cur)
			}
			reinstall, display := "", "nvm install "+target
			if cfg.Node.ReinstallPackages {
				reinstall = "yes"
				display += " --reinstall-packages-from=current"
			}
			display += " (skipped if already current) && nvm alias default " + target
			// nvm refuses --reinstall-packages-from=current when the target is the
			// current version, so only install when the newest release differs.
			script := nvmScript(`
cur=$(nvm current)
latest=$(nvm version-remote "$1" 2>/dev/null)
if [ -n "$latest" ] && [ "$latest" = "$cur" ]; then
    echo "Node $cur is already the newest release for $1."
else
    if [ -n "$2" ] && [ -n "$latest" ] && [ "$cur" != none ] && [ "$cur" != system ]; then
        nvm install "$1" --reinstall-packages-from=current || exit
    else
        nvm install "$1" || exit
    fi
fi
nvm alias default "$1" >/dev/null || exit
echo "nvm default: $(nvm version default)"`)
			act := Shell("nvm install", display, script, target, reinstall)
			inner := act.Run
			act.Run = func(ctx context.Context, out io.Writer) error {
				if err := inner(ctx, out); err != nil {
					return err
				}
				useDefaultNode(ctx) // later steps (pnpm) should see the new node
				return nil
			}
			return []Action{act}, nil
		},
	}
}

// useDefaultNode puts the nvm default node's bin dir first on our PATH.
func useDefaultNode(ctx context.Context) {
	out, err := Output(ctx, "bash", "-c", nvmScript(`nvm which default`))
	if err != nil || out == "" {
		return
	}
	bin := filepath.Dir(firstLine(out))
	os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// ---------- pnpm ----------

type npmLatest struct {
	Version string `json:"version"`
}

func pnpmStep() *Step {
	return &Step{
		ID: "pnpm", Name: "pnpm + globals", DefaultOn: true,
		Description: "pnpm self-update, then pnpm update -g for global packages.",
		Available:   func() bool { return Have("pnpm") },
		Version: func(ctx context.Context, _ *config.Config) string {
			out, _ := Output(ctx, "pnpm", "--version")
			return out
		},
		Check: func(ctx context.Context, _ *config.Config) CheckResult {
			var details []string
			cur, _ := Output(ctx, "pnpm", "--version")
			var latest npmLatest
			if err := GetJSON(ctx, "https://registry.npmjs.org/pnpm/latest", &latest); err == nil &&
				cur != "" && compareVersions(latest.Version, cur) > 0 {
				details = append(details, fmt.Sprintf("pnpm %s → %s", cur, latest.Version))
			}
			var pkgs map[string]struct {
				Current string `json:"current"`
				Latest  string `json:"latest"`
			}
			out, _ := Output(ctx, "pnpm", "outdated", "-g", "--format", "json") // exits 1 when outdated
			if err := json.Unmarshal([]byte(out), &pkgs); err == nil {
				for name, p := range pkgs {
					details = append(details, fmt.Sprintf("%s %s → %s", name, p.Current, p.Latest))
				}
			}
			if len(details) == 0 {
				return upToDate()
			}
			return outdated(plural(len(details), "update"), details...)
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			return []Action{
				pnpmSelfUpdate(ctx),
				Exec("pnpm update -g", false, nil, "pnpm", "update", "-g"),
			}, nil
		},
	}
}

// ---------- uv ----------

func uvStep() *Step {
	return &Step{
		ID: "uv", Name: "uv + tools", DefaultOn: true,
		Description: "uv self update, then uv tool upgrade --all.",
		Available:   func() bool { return Have("uv") },
		Version: func(ctx context.Context, cfg *config.Config) string {
			out, _ := Output(ctx, "uv", "--version")
			f := strings.Fields(out)
			if len(f) > 1 {
				return f[1]
			}
			return out
		},
		Check: func(ctx context.Context, _ *config.Config) CheckResult {
			var details []string
			if out, _ := CombinedOutput(ctx, "uv", "self", "update", "--dry-run"); strings.Contains(out, "Would update") {
				for _, l := range strings.Split(out, "\n") {
					if strings.Contains(l, "Would update") {
						details = append(details, strings.TrimSpace(strings.TrimPrefix(l, "info:")))
					}
				}
			}
			out, err := Output(ctx, "uv", "tool", "list", "--outdated")
			if err == nil {
				for _, l := range strings.Split(out, "\n") {
					if l != "" && !strings.HasPrefix(l, "-") && !strings.HasPrefix(l, " ") {
						details = append(details, l)
					}
				}
			}
			if len(details) == 0 {
				return upToDate()
			}
			return outdated(plural(len(details), "update"), details...)
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			return []Action{
				Exec("uv self update", false, nil, "uv", "self", "update"),
				Exec("uv tool upgrade", false, nil, "uv", "tool", "upgrade", "--all"),
			}, nil
		},
	}
}

// ---------- pyenv ----------

func pyenvRoot() string {
	if r := os.Getenv("PYENV_ROOT"); r != "" {
		return r
	}
	return filepath.Join(home(), ".pyenv")
}

func pyenvStep() *Step {
	return &Step{
		ID: "pyenv", Name: "pyenv + plugins", DefaultOn: true,
		Description: "pyenv update (pyenv-update plugin) or git pull --ff-only of the pyenv checkout.",
		Available:   func() bool { _, err := os.Stat(filepath.Join(pyenvRoot(), ".git")); return err == nil },
		Version: func(ctx context.Context, cfg *config.Config) string {
			out, _ := Output(ctx, "git", "-C", pyenvRoot(), "describe", "--tags")
			return out
		},
		Check: func(ctx context.Context, _ *config.Config) CheckResult {
			root := pyenvRoot()
			if _, err := CombinedOutput(ctx, "git", "-C", root, "fetch", "-q"); err != nil {
				return errResult(fmt.Errorf("git fetch: %w", err))
			}
			n, err := Output(ctx, "git", "-C", root, "rev-list", "--count", "HEAD..@{u}")
			if err != nil {
				return errResult(err)
			}
			if n == "0" {
				return upToDate()
			}
			return outdated(n + " new commit(s)")
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			if Have("pyenv") {
				if out, _ := Output(ctx, "pyenv", "commands"); contains(strings.Split(out, "\n"), "update") {
					return []Action{Exec("pyenv update", false, nil, "pyenv", "update")}, nil
				}
			}
			return []Action{Exec("pyenv git pull", false, nil, "git", "-C", pyenvRoot(), "pull", "--ff-only")}, nil
		},
	}
}

// ---------- Claude Code ----------

func claudeStep() *Step {
	return &Step{
		ID: "claude", Name: "Claude Code", DefaultOn: true,
		Description: "claude update.",
		Available:   func() bool { return Have("claude") },
		Version: func(ctx context.Context, cfg *config.Config) string {
			out, _ := Output(ctx, "claude", "--version")
			return strings.Fields(out + " ")[0]
		},
		Check: func(ctx context.Context, _ *config.Config) CheckResult {
			out, _ := Output(ctx, "claude", "--version")
			cur := strings.Fields(out + " ")[0]
			var latest npmLatest
			if err := GetJSON(ctx, "https://registry.npmjs.org/@anthropic-ai%2fclaude-code/latest", &latest); err != nil {
				return errResult(err)
			}
			if cur == "" || compareVersions(latest.Version, cur) <= 0 {
				return upToDate()
			}
			return outdated(cur + " → " + latest.Version)
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			return []Action{Exec("claude update", false, nil, "claude", "update")}, nil
		},
	}
}

// ---------- custom ----------

func customStep(c config.CustomStep) *Step {
	desc := c.Description
	if desc == "" {
		desc = "Custom step: " + c.Command
	}
	return &Step{
		ID: c.ID, Name: c.Name, Description: desc, NeedsSudo: c.Sudo, DefaultOn: true,
		Available: func() bool { return c.Requires == "" || Have(c.Requires) },
		Version: func(ctx context.Context, cfg *config.Config) string {
			if c.Version == "" {
				return ""
			}
			out, _ := Output(ctx, "bash", "-c", c.Version)
			return firstLine(out)
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			if c.Sudo {
				return []Action{Exec(c.Name, true, nil, "bash", "-c", c.Command)}, nil
			}
			return []Action{Shell(c.Name, c.Command, c.Command)}, nil
		},
	}
}

// ---------- helpers ----------

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	if strings.HasSuffix(word, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.TrimSpace(x) == s {
			return true
		}
	}
	return false
}

// withCleanup adds a cleanup report to a check's result.
func withCleanup(check func(context.Context, *config.Config) CheckResult, clean func(context.Context) []Cleanup) func(context.Context, *config.Config) CheckResult {
	return func(ctx context.Context, cfg *config.Config) CheckResult {
		r := check(ctx, cfg)
		r.Cleanup = clean(ctx)
		return r
	}
}

// nodeCleanup reports Node versions installed by nvm other than the one in use.
// Upgrading with nvm leaves the previous version behind.
func nodeCleanup(ctx context.Context) []Cleanup {
	cur, _ := Output(ctx, "node", "--version")
	entries, err := os.ReadDir(filepath.Join(nvmDir(), "versions", "node"))
	if err != nil {
		return nil
	}
	var out []Cleanup
	for _, e := range entries {
		if v := e.Name(); e.IsDir() && v != cur {
			out = append(out, Cleanup{
				Text:   "old Node " + v + " is still installed",
				Action: Shell("nvm uninstall "+v, "nvm uninstall "+v, nvmPrelude+`nvm uninstall "$1"`, v),
			})
		}
	}
	return out
}

// pnpmSelfUpdate asks the registry for pnpm's latest version and passes it to
// `pnpm self-update` explicitly. Without a version, pnpm can answer from its
// metadata cache and wrongly report an older release as the latest.
func pnpmSelfUpdate(ctx context.Context) Action {
	var latest npmLatest
	cur, _ := Output(ctx, "pnpm", "--version")
	// Only pin when it is an upgrade: an explicit version would also allow a downgrade.
	if err := GetJSON(ctx, "https://registry.npmjs.org/pnpm/latest", &latest); err != nil || latest.Version == "" ||
		cur == "" || compareVersions(latest.Version, cur) <= 0 {
		return Exec("pnpm self-update", false, nil, "pnpm", "self-update")
	}
	return Exec("pnpm self-update", false, nil, "pnpm", "self-update", latest.Version)
}
