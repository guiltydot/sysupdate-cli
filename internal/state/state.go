// Package state persists run history between invocations.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

// StepState is the last outcome of one step.
type StepState struct {
	LastRun  time.Time     `json:"last_run"`
	OK       bool          `json:"ok"`
	Duration time.Duration `json:"duration"`
	LastOK   time.Time     `json:"last_ok,omitempty"`
}

// State is stored in $XDG_STATE_HOME/sysupdate/state.json.
type State struct {
	LastRun time.Time            `json:"last_run"`
	LastLog string               `json:"last_log"`
	Steps   map[string]StepState `json:"steps"`
}

func path() string { return filepath.Join(config.StateDir(), "state.json") }

// Load returns the saved state, or an empty one.
func Load() *State {
	s := &State{Steps: map[string]StepState{}}
	if b, err := os.ReadFile(path()); err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Steps == nil {
		s.Steps = map[string]StepState{}
	}
	return s
}

// Record stores a step outcome.
func (s *State) Record(id string, ok bool, d time.Duration, at time.Time) {
	prev := s.Steps[id]
	st := StepState{LastRun: at, OK: ok, Duration: d, LastOK: prev.LastOK}
	if ok {
		st.LastOK = at
	}
	s.Steps[id] = st
}

// Save writes the state file.
func (s *State) Save() error {
	if err := os.MkdirAll(filepath.Dir(path()), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), b, 0o600)
}
