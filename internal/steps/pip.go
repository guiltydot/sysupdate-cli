package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

// pipEnv keeps pip from printing its own "new release available" notice.
var pipEnv = []string{"PIP_DISABLE_PIP_VERSION_CHECK=1"}

// pyenvPython is one Python that pyenv installed.
type pyenvPython struct{ version, python string }

// pyenvPythons lists the Pythons under $PYENV_ROOT/versions that have pip.
// Virtualenvs (pyenv-virtualenv symlinks them into versions/) are skipped:
// their packages belong to a project, not to the machine.
func pyenvPythons() []pyenvPython {
	dir := filepath.Join(pyenvRoot(), "versions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []pyenvPython
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		py := filepath.Join(dir, e.Name(), "bin", "python")
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "bin", "pip")); err != nil {
			continue
		}
		out = append(out, pyenvPython{e.Name(), py})
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i].version, out[j].version) > 0 })
	return out
}

// pipArgv runs `python -m pip args...` with pipEnv set, for the quiet Output helpers.
func pipArgv(python string, args ...string) []string {
	return append(append(append([]string{"env"}, pipEnv...), python, "-m", "pip"), args...)
}

type pipOutdated struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Latest  string `json:"latest_version"`
}

// outdatedPip lists packages that nothing else depends on (pip itself and what
// you installed) and that have a newer release. Their dependencies are
// upgraded by pip only when the new version needs it.
func outdatedPip(ctx context.Context, python string) ([]pipOutdated, error) {
	out, err := OutputTimeout(ctx, 90*time.Second, pipArgv(python, "list", "--outdated", "--not-required", "--format=json")...)
	if err != nil {
		return nil, fmt.Errorf("pip list: %w", err)
	}
	return parsePipOutdated(out)
}

func parsePipOutdated(out string) ([]pipOutdated, error) {
	var list []pipOutdated
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("pip list: %w", err)
	}
	return list, nil
}

func pipStep() *Step {
	return &Step{
		ID: "pip", Name: "pip packages", DefaultOn: true,
		Description: "For each pyenv Python: upgrades pip, then the packages you installed with pip (not their dependencies, and not pip.exclude). Virtualenvs and the system Python are left alone.",
		Available:   func() bool { return len(pyenvPythons()) > 0 },
		Version: func(ctx context.Context, _ *config.Config) string {
			pys := pyenvPythons()
			if len(pys) != 1 {
				return plural(len(pys), "Python")
			}
			out, _ := Output(ctx, pipArgv(pys[0].python, "--version")...)
			if f := strings.Fields(out); len(f) > 1 {
				return pys[0].version + " · pip " + f[1]
			}
			return pys[0].version
		},
		Check: func(ctx context.Context, cfg *config.Config) CheckResult {
			pys := pyenvPythons()
			var ups, errs []string
			for _, py := range pys {
				list, err := outdatedPip(ctx, py.python)
				if err != nil {
					errs = append(errs, py.version+": "+err.Error())
					continue
				}
				for _, p := range list {
					d := fmt.Sprintf("%s %s → %s", p.Name, p.Version, p.Latest)
					if len(pys) > 1 {
						d = py.version + ": " + d
					}
					if contains(cfg.Pip.Exclude, p.Name) && !strings.EqualFold(p.Name, "pip") {
						d += " (excluded)"
					}
					ups = append(ups, d)
				}
			}
			if len(ups) == 0 && len(errs) > 0 {
				return CheckResult{State: CheckError, Summary: "check failed", Details: errs}
			}
			if len(ups) == 0 {
				return upToDate()
			}
			return outdated(plural(len(ups), "package"), append(ups, errs...)...)
		},
		Plan: func(ctx context.Context, cfg *config.Config) ([]Action, error) {
			var acts []Action
			for _, py := range pyenvPythons() {
				acts = append(acts, Exec("pip "+py.version+": pip", false, pipEnv,
					py.python, "-m", "pip", "install", "--upgrade", "pip"))
				list, err := outdatedPip(ctx, py.python)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", py.version, err)
				}
				var names []string
				for _, p := range list {
					if !strings.EqualFold(p.Name, "pip") && !contains(cfg.Pip.Exclude, p.Name) {
						names = append(names, p.Name)
					}
				}
				if len(names) > 0 {
					acts = append(acts, Exec("pip "+py.version+": packages", false, pipEnv,
						py.python, append([]string{"-m", "pip", "install", "--upgrade"}, names...)...))
				}
			}
			return acts, nil
		},
	}
}
