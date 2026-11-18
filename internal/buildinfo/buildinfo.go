// Package buildinfo exposes the sysupdate version to the steps and command
// packages without coupling them to main.
package buildinfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/vercmp"
)

// Version is set by main at startup.
var Version = "dev"

type Kind int

const (
	Local Kind = iota
	Release
	GoInstall
)

func (k Kind) String() string {
	switch k {
	case Release:
		return "release"
	case GoInstall:
		return "go install"
	default:
		return "local"
	}
}

var (
	// vMAJOR.MINOR.PATCH with an optional prerelease, as the release workflow tags.
	releaseRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	// git describe between tags: v0.4.0-3-gabc1234
	describeRe = regexp.MustCompile(`-\d+-g[0-9a-f]+$`)
	// Go pseudo-versions: ...-20261118163223-7fadc48c2af0 or ....20261118163223-7fadc48c2af0
	pseudoRe = regexp.MustCompile(`[.-]\d{14}-[0-9a-f]{12}$`)
)

// Classify reports whether the running binary is a Release binary that
// self-update may replace, a local build, or a `go install` binary.
//
// The version string alone cannot tell a Release binary from `go install
// ...@vX`: since Go 1.24 both carry the tag in their build info. So a release
// version is classified as GoInstall when the binary sits in GOBIN or
// GOPATH/bin, where `go install` puts it.
func Classify(version string) Kind {
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
	}
	return classify(version, exe, goBinDirs())
}

func classify(version, exe string, goBins []string) Kind {
	v := strings.TrimSpace(version)
	if !releaseRe.MatchString(v) || strings.HasSuffix(v, "-dirty") ||
		describeRe.MatchString(v) || pseudoRe.MatchString(v) || strings.HasPrefix(v, "v0.0.0-") {
		return Local
	}
	if exe != "" {
		dir := filepath.Dir(exe)
		for _, b := range goBins {
			if b == "" {
				continue
			}
			if r, err := filepath.EvalSymlinks(b); err == nil {
				b = r
			}
			if filepath.Clean(b) == dir {
				return GoInstall
			}
		}
	}
	return Release
}

// goBinDirs lists the directories `go install` writes to. It asks `go env`
// (which also reads `go env -w` settings) and falls back to the environment.
var goBinDirs = sync.OnceValue(func() []string {
	gobin, gopath := os.Getenv("GOBIN"), os.Getenv("GOPATH")
	if goExe, err := exec.LookPath("go"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, goExe, "env", "GOBIN", "GOPATH").Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) == 2 {
				gobin, gopath = strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
			}
		}
	}
	if gobin != "" {
		return []string{gobin}
	}
	if gopath == "" {
		if h, err := os.UserHomeDir(); err == nil {
			gopath = filepath.Join(h, "go")
		}
	}
	var dirs []string
	for _, p := range filepath.SplitList(gopath) {
		dirs = append(dirs, filepath.Join(p, "bin"))
	}
	return dirs
})

// Newer reports whether tag is a newer version than current.
//
// A git-describe version such as v0.4.0-3-gabc1234 is built from commits after
// v0.4.0, so it counts as newer than v0.4.0 (SemVer would sort it before, as a
// prerelease). A -dirty suffix is ignored: v0.4.0-dirty is not older than v0.4.0.
func Newer(tag, current string) bool {
	cur := strings.TrimSpace(current)
	cur = strings.TrimSuffix(strings.TrimSuffix(cur, "-dirty"), "+dirty")
	if loc := describeRe.FindStringIndex(cur); loc != nil {
		return vercmp.Compare(tag, cur[:loc[0]]) > 0
	}
	return vercmp.Compare(tag, cur) > 0
}
