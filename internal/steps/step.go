// Package steps defines the update steps sysupdate knows how to run.
package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

// Step is one updater (APT, Go, Rust, ...).
type Step struct {
	ID          string
	Name        string
	Description string
	NeedsSudo   bool
	DefaultOn   bool

	// Available reports whether the tool is installed on this machine.
	Available func() bool
	// Version returns the currently installed version (short, one line).
	Version func(ctx context.Context, cfg *config.Config) string
	// Check looks for pending updates without changing anything. May be nil.
	Check func(ctx context.Context, cfg *config.Config) CheckResult
	// Plan returns the actions to run, resolved at run time.
	Plan func(ctx context.Context, cfg *config.Config) ([]Action, error)
}

// CheckState summarizes a Check.
type CheckState int

const (
	CheckUnknown CheckState = iota
	CheckUpToDate
	CheckOutdated
	CheckError
)

// CheckResult is what a Check found.
type CheckResult struct {
	State   CheckState
	Summary string   // short, e.g. "3 packages" or "1.25.1 → 1.25.2"
	Details []string // e.g. the package list, shown in the detail pane
	// Cleanup lists things that could be removed (unused packages, old versions).
	// They are only removed when the user asks for it (`d` in the TUI, `sysupdate clean`).
	Cleanup []Cleanup
}

// Cleanup is one removable leftover and the command that removes it.
type Cleanup struct {
	Text   string // what is left over, e.g. "old Node v26.13.0"
	Sudo   bool
	Action Action // Display is the command shown to the user
}

// CleanupStep wraps a step's cleanup items in a step the runner can run.
func CleanupStep(s *Step, items []Cleanup) *Step {
	sudo := false
	for _, c := range items {
		sudo = sudo || c.Sudo
	}
	return &Step{
		ID: s.ID + "-cleanup", Name: "Clean up " + s.Name, NeedsSudo: sudo,
		Description: "Removes leftovers found by the " + s.Name + " check.",
		Available:   func() bool { return true },
		Version:     func(context.Context, *config.Config) string { return "" },
		Plan: func(context.Context, *config.Config) ([]Action, error) {
			acts := make([]Action, len(items))
			for i, c := range items {
				acts[i] = c.Action
			}
			return acts, nil
		},
	}
}

// Action is a single unit of work inside a step.
type Action struct {
	Label   string
	Display string // command line shown in previews
	Run     func(ctx context.Context, out io.Writer) error
}

// Have reports whether a binary is on PATH.
func Have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Exec builds an action that runs a command, optionally through `sudo -n`.
func Exec(label string, sudo bool, env []string, name string, args ...string) Action {
	argv := append([]string{name}, args...)
	if sudo {
		pre := []string{"sudo", "-n"}
		if len(env) > 0 {
			pre = append(pre, "env")
			pre = append(pre, env...)
		}
		argv = append(pre, argv...)
		env = nil
	}
	return Action{
		Label:   label,
		Display: strings.Join(argv, " "),
		Run: func(ctx context.Context, out io.Writer) error {
			return RunCmd(ctx, out, env, argv...)
		},
	}
}

// Shell builds an action that runs a bash script; extra args become $1, $2, ...
func Shell(label, display, script string, args ...string) Action {
	if display == "" {
		display = script
	}
	return Action{
		Label:   label,
		Display: display,
		Run: func(ctx context.Context, out io.Writer) error {
			argv := append([]string{"bash", "-c", script, "sysupdate"}, args...)
			return RunCmd(ctx, out, nil, argv...)
		},
	}
}

// RunCmd runs argv with stdout/stderr going to out and stdin closed.
// The child gets its own process group so cancelling kills the whole tree.
func RunCmd(ctx context.Context, out io.Writer, env []string, argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		return ctx.Err()
	}
}

// Output runs a command quietly and returns trimmed stdout (stderr discarded).
func Output(ctx context.Context, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// OutputTimeout is Output with a caller-chosen timeout, for slower commands.
func OutputTimeout(ctx context.Context, d time.Duration, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// CombinedOutput is Output but includes stderr.
func CombinedOutput(ctx context.Context, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// GetJSON fetches url and decodes the JSON body into v.
func GetJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "sysupdate-cli")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// getText fetches url and returns the body as a string.
func getText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sysupdate-cli")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func errResult(err error) CheckResult {
	return CheckResult{State: CheckError, Summary: "check failed", Details: []string{err.Error()}}
}

func upToDate() CheckResult { return CheckResult{State: CheckUpToDate, Summary: "up to date"} }

func outdated(summary string, details ...string) CheckResult {
	return CheckResult{State: CheckOutdated, Summary: summary, Details: details}
}

// All returns the built-in steps followed by the custom ones from cfg, in run order.
func All(cfg *config.Config) []*Step {
	list := []*Step{
		aptStep(), goStep(), gupStep(), rustupStep(), cargoStep(), nodeStep(),
		pnpmStep(), uvStep(), pyenvStep(), claudeStep(), ghExtStep(), dockerStep(),
	}
	for _, c := range cfg.Custom {
		list = append(list, customStep(c))
	}
	return list
}
