// Package config loads and saves the sysupdate YAML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the user-editable configuration.
type Config struct {
	// Enabled maps a step ID to whether it runs. Steps missing here use their default.
	Enabled map[string]bool `yaml:"enabled"`

	Apt     AptConfig     `yaml:"apt"`
	Go      GoConfig      `yaml:"go"`
	Cargo   CargoConfig   `yaml:"cargo"`
	Gup     GupConfig     `yaml:"gup"`
	Pip     PipConfig     `yaml:"pip"`
	Docker  DockerConfig  `yaml:"docker"`
	Node    NodeConfig    `yaml:"node"`
	General GeneralConfig `yaml:"general"`

	// Custom are user-defined steps run after the built-in ones.
	Custom []CustomStep `yaml:"custom"`
}

type AptConfig struct {
	FullUpgrade bool `yaml:"full_upgrade"` // apt-get full-upgrade instead of upgrade
	Autoremove  bool `yaml:"autoremove"`   // apt-get autoremove --purge afterwards
	Autoclean   bool `yaml:"autoclean"`    // apt-get autoclean afterwards
}

type GoConfig struct {
	Root string `yaml:"root"` // install location, default /usr/local/go
}

type CargoConfig struct {
	Exclude []string `yaml:"exclude"` // crates never reinstalled
}

type DockerConfig struct {
	Exclude []string `yaml:"exclude"` // images (repo:tag or repo) that are never pulled
}

type GupConfig struct {
	Exclude []string `yaml:"exclude"` // binaries in $GOBIN that gup leaves alone
}

type PipConfig struct {
	Exclude []string `yaml:"exclude"` // packages the pip step never upgrades
}

type NodeConfig struct {
	// Channel is "current-major" (stay on the installed major) or "lts" (latest LTS).
	Channel           string `yaml:"channel"`
	ReinstallPackages bool   `yaml:"reinstall_packages"` // --reinstall-packages-from=current
}

type GeneralConfig struct {
	StopOnFailure bool `yaml:"stop_on_failure"` // abort the remaining steps after a failure
	CheckOnStart  bool `yaml:"check_on_start"`  // look for available updates when the TUI opens
	KeepLogs      int  `yaml:"keep_logs"`       // number of run logs to keep
}

// CustomStep is a shell command run with bash -c.
type CustomStep struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Command     string `yaml:"command"`
	Sudo        bool   `yaml:"sudo,omitempty"`     // run the command via sudo
	Version     string `yaml:"version,omitempty"`  // optional command printing the current version
	Requires    string `yaml:"requires,omitempty"` // optional binary that must be on PATH
}

// Default returns the configuration used when no file exists.
func Default() *Config {
	return &Config{
		Enabled: map[string]bool{},
		Apt:     AptConfig{FullUpgrade: true},
		Go:      GoConfig{Root: "/usr/local/go"},
		Node:    NodeConfig{Channel: "current-major", ReinstallPackages: true},
		General: GeneralConfig{CheckOnStart: true, KeepLogs: 20},
		Custom:  []CustomStep{},
	}
}

// Path returns the config file location ($XDG_CONFIG_HOME/sysupdate/config.yaml).
func Path() string {
	if p := os.Getenv("SYSUPDATE_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "sysupdate", "config.yaml")
}

// Exists reports whether the config file has been written yet. Until then
// sysupdate treats the machine as new and offers the first-run setup.
func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

// StateDir returns $XDG_STATE_HOME/sysupdate, used for logs and run history.
func StateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "sysupdate")
}

// Load reads the config file, falling back to defaults when it does not exist.
func Load() (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", Path(), err)
	}
	cfg.normalize()
	return cfg, nil
}

func (c *Config) normalize() {
	if c.Enabled == nil {
		c.Enabled = map[string]bool{}
	}
	if c.Go.Root == "" {
		c.Go.Root = "/usr/local/go"
	}
	if c.Node.Channel != "lts" {
		c.Node.Channel = "current-major"
	}
	if c.General.KeepLogs <= 0 {
		c.General.KeepLogs = 20
	}
	for i := range c.Custom {
		if c.Custom[i].ID == "" {
			c.Custom[i].ID = fmt.Sprintf("custom-%d", i+1)
		}
		if c.Custom[i].Name == "" {
			c.Custom[i].Name = c.Custom[i].ID
		}
	}
}

// Save writes the config atomically, creating the directory if needed.
func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	header := []byte("# sysupdate configuration. Edit freely; the TUI rewrites this file when you change settings.\n")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(header, data...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// IsEnabled reports whether a step runs, using def when the config is silent.
func (c *Config) IsEnabled(id string, def bool) bool {
	if v, ok := c.Enabled[id]; ok {
		return v
	}
	return def
}
