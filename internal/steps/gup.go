package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

// gupBinary is one entry of `gup check --json`.
type gupBinary struct {
	Name               string `json:"name"`
	CurrentVersion     string `json:"current_version"`
	LatestVersion      string `json:"latest_version"`
	CurrentGoVersion   string `json:"current_go_version"`   // the Go toolchain installed now
	InstalledGoVersion string `json:"installed_go_version"` // the Go the binary was built with
	Status             string `json:"status"`
}

func gupStep() *Step {
	return &Step{
		ID: "gup", Name: "Go binaries (gup)", DefaultOn: true,
		Description: "gup update: rebuilds `go install` binaries in $GOBIN that are outdated or were built with an older Go. Runs after the Go step so they pick up the new toolchain.",
		Available:   func() bool { return Have("gup") },
		Version: func(ctx context.Context, _ *config.Config) string {
			out, err := Output(ctx, "gup", "list", "--no-color")
			if err != nil || out == "" {
				return ""
			}
			return plural(len(strings.Split(out, "\n")), "binary")
		},
		Check: func(ctx context.Context, cfg *config.Config) CheckResult {
			out, err := OutputTimeout(ctx, 90*time.Second, "gup", "check", "--json", "--no-color")
			var bins []gupBinary
			if jerr := json.Unmarshal([]byte(out), &bins); jerr != nil {
				if err == nil {
					err = jerr
				}
				return errResult(fmt.Errorf("gup check: %w", err))
			}
			var ups []string
			for _, b := range bins {
				if b.Status == "up-to-date" {
					continue
				}
				detail := fmt.Sprintf("%s %s → %s", b.Name, b.CurrentVersion, b.LatestVersion)
				if b.CurrentVersion == b.LatestVersion && b.InstalledGoVersion != b.CurrentGoVersion {
					detail = fmt.Sprintf("%s rebuild (%s → %s)", b.Name, b.InstalledGoVersion, b.CurrentGoVersion)
				} else if b.Status != "update-available" && b.Status != "" {
					detail += " (" + b.Status + ")"
				}
				if contains(cfg.Gup.Exclude, b.Name) {
					detail += " (excluded)"
				}
				ups = append(ups, detail)
			}
			if len(ups) == 0 {
				return upToDate()
			}
			return outdated(plural(len(ups), "binary"), ups...)
		},
		Plan: func(ctx context.Context, cfg *config.Config) ([]Action, error) {
			args := []string{"update", "--no-color"}
			var ex []string
			for _, e := range cfg.Gup.Exclude {
				if e = strings.TrimSpace(e); e != "" {
					ex = append(ex, e)
				}
			}
			if len(ex) > 0 {
				args = append(args, "--exclude", strings.Join(ex, ","))
			}
			return []Action{Exec("gup update", false, nil, "gup", args...)}, nil
		},
	}
}
