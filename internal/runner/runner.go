// Package runner executes update steps sequentially and reports progress as events.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/config"
	"github.com/guiltydot/sysupdate-cli/internal/state"
	"github.com/guiltydot/sysupdate-cli/internal/steps"
)

// Events emitted while running. Idx is the index into the step list passed to Run.
type (
	StepStart struct {
		Idx     int
		Actions []steps.Action
	}
	ActionStart struct {
		Idx            int
		Label, Display string
	}
	Line struct {
		Idx     int
		Text    string
		Replace bool // overwrite the previous line (carriage-return progress output)
	}
	StepDone struct{ Result Result }
	RunDone  struct {
		Results        []Result
		LogPath        string
		RebootRequired bool
		Cancelled      bool
	}
)

// Status of a finished step.
type Status int

const (
	OK Status = iota
	Failed
	Skipped
	Cancelled
)

func (s Status) String() string {
	return [...]string{"ok", "failed", "skipped", "cancelled"}[s]
}

// Result is the outcome of one step.
type Result struct {
	Idx      int
	ID, Name string
	Status   Status
	Failed   []string // labels of failed actions
	Err      string
	Duration time.Duration
}

// NeedsSudo reports whether any of the steps uses sudo.
func NeedsSudo(list []*steps.Step) bool {
	for _, s := range list {
		if s.NeedsSudo {
			return true
		}
	}
	return false
}

// Run executes the steps in order, calling emit for each event (from this goroutine).
func Run(ctx context.Context, cfg *config.Config, list []*steps.Step, emit func(any)) RunDone {
	logPath, logFile := openLog(cfg)
	if logFile != nil {
		defer logFile.Close()
	}
	logw := io.Writer(io.Discard)
	if logFile != nil {
		logw = logFile
	}
	fmt.Fprintf(logw, "sysupdate run started %s\n", time.Now().Format(time.RFC1123))

	if NeedsSudo(list) {
		stop := keepSudoAlive()
		defer stop()
	}

	st := state.Load()
	results := make([]Result, len(list))
	stopRest := false
	for i, s := range list {
		res := Result{Idx: i, ID: s.ID, Name: s.Name}
		start := time.Now()
		switch {
		case ctx.Err() != nil:
			res.Status, res.Err = Cancelled, "cancelled"
		case stopRest:
			res.Status, res.Err = Skipped, "skipped after an earlier failure"
		case !s.Available():
			res.Status, res.Err = Skipped, "not installed"
		default:
			res = runStep(ctx, cfg, i, s, emit, logw)
		}
		res.Duration = time.Since(start)
		results[i] = res
		emit(StepDone{Result: res})
		fmt.Fprintf(logw, "--- %s: %s (%s) %s\n", s.Name, res.Status, res.Duration.Round(time.Millisecond), res.Err)
		if res.Status == OK || res.Status == Failed {
			st.Record(s.ID, res.Status == OK, res.Duration, start)
		}
		if res.Status == Failed && cfg.General.StopOnFailure {
			stopRest = true
		}
	}

	done := RunDone{Results: results, LogPath: logPath, Cancelled: ctx.Err() != nil}
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		done.RebootRequired = true
	}
	st.LastRun = time.Now()
	st.LastLog = logPath
	_ = st.Save()
	pruneLogs(cfg.General.KeepLogs)
	return done
}

func runStep(ctx context.Context, cfg *config.Config, idx int, s *steps.Step, emit func(any), logw io.Writer) Result {
	res := Result{Idx: idx, ID: s.ID, Name: s.Name}
	fmt.Fprintf(logw, "\n=== %s ===\n", s.Name)
	acts, err := s.Plan(ctx, cfg)
	emit(StepStart{Idx: idx, Actions: acts})
	if err != nil {
		res.Status, res.Err = Failed, err.Error()
		emit(Line{Idx: idx, Text: "!! " + err.Error()})
		fmt.Fprintln(logw, "!! "+err.Error())
		return res
	}
	if len(acts) == 0 {
		emit(Line{Idx: idx, Text: "nothing to do"})
		return res
	}
	for _, a := range acts {
		if ctx.Err() != nil {
			res.Status, res.Err = Cancelled, "cancelled"
			return res
		}
		emit(ActionStart{Idx: idx, Label: a.Label, Display: a.Display})
		emit(Line{Idx: idx, Text: "$ " + a.Display})
		fmt.Fprintln(logw, "$ "+a.Display)
		lw := &lineWriter{idx: idx, emit: emit, log: logw}
		err := a.Run(ctx, lw)
		lw.Flush()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				res.Status, res.Err = Cancelled, "cancelled"
				return res
			}
			msg := fmt.Sprintf("!! %s failed: %v", a.Label, err)
			emit(Line{Idx: idx, Text: msg})
			fmt.Fprintln(logw, msg)
			res.Failed = append(res.Failed, a.Label)
		}
	}
	if len(res.Failed) > 0 {
		res.Status = Failed
		res.Err = "failed: " + strings.Join(res.Failed, ", ")
	}
	return res
}

// keepSudoAlive refreshes the sudo timestamp until stopped so long steps
// don't lose authentication halfway.
func keepSudoAlive() func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = exec.Command("sudo", "-n", "-v").Run()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// SudoCached reports whether sudo can run without a password right now.
func SudoCached() bool { return exec.Command("sudo", "-n", "true").Run() == nil }

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07`)

// lineWriter splits command output into lines, treating \r as "replace the
// current line" so progress bars update in place.
type lineWriter struct {
	idx      int
	emit     func(any)
	log      io.Writer
	cur      []byte
	live     bool // the last emitted line is a \r-terminated line still being updated
	lastLive string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		switch b {
		case '\n':
			if len(w.cur) == 0 && w.live { // \r\n: keep the live line as final
				w.live = false
				continue
			}
			w.send(false)
		case '\r':
			w.send(true)
		default:
			w.cur = append(w.cur, b)
		}
	}
	return len(p), nil
}

func (w *lineWriter) send(cr bool) {
	text := ansiRE.ReplaceAllString(string(w.cur), "")
	w.cur = w.cur[:0]
	if cr && text == "" {
		return
	}
	w.emit(Line{Idx: w.idx, Text: text, Replace: w.live})
	if !cr {
		fmt.Fprintln(w.log, text)
	}
	w.live = cr
	if cr {
		w.lastLive = text
	}
}

func (w *lineWriter) Flush() {
	if len(w.cur) > 0 {
		w.send(false)
	} else if w.live {
		fmt.Fprintln(w.log, w.lastLive)
	}
	w.live = false
}

func logDir() string { return filepath.Join(config.StateDir(), "logs") }

func openLog(cfg *config.Config) (string, *os.File) {
	if err := os.MkdirAll(logDir(), 0o700); err != nil {
		return "", nil
	}
	p := filepath.Join(logDir(), time.Now().Format("2006-01-02_15-04-05")+".log")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // logs can contain command output
	if err != nil {
		return "", nil
	}
	return p, f
}

// Logs returns run log paths, newest first.
func Logs() []string {
	m, _ := filepath.Glob(filepath.Join(logDir(), "*.log"))
	sort.Sort(sort.Reverse(sort.StringSlice(m)))
	return m
}

func pruneLogs(keep int) {
	for _, p := range Logs()[min(keep, len(Logs())):] {
		_ = os.Remove(p)
	}
}

// ReadLog returns a log's content (for the log viewer).
func ReadLog(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return err.Error()
	}
	return string(bytes.TrimRight(b, "\n"))
}
