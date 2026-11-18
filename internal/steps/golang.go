package steps

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/guiltydot/sysupdate-cli/internal/config"
	"github.com/guiltydot/sysupdate-cli/internal/fetch"
)

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Kind     string `json:"kind"`
		SHA256   string `json:"sha256"`
	} `json:"files"`
}

type goTarball struct{ version, file, sha string }

func latestGo(ctx context.Context) (goTarball, error) {
	var rels []goRelease
	if err := GetJSON(ctx, "https://go.dev/dl/?mode=json", &rels); err != nil {
		return goTarball{}, err
	}
	for _, r := range rels {
		if !r.Stable {
			continue
		}
		for _, f := range r.Files {
			if f.OS == "linux" && f.Arch == runtime.GOARCH && f.Kind == "archive" {
				return goTarball{r.Version, f.Filename, f.SHA256}, nil
			}
		}
	}
	return goTarball{}, fmt.Errorf("no linux/%s release in the go.dev feed", runtime.GOARCH)
}

func installedGo(ctx context.Context, root string) string {
	out, err := Output(ctx, filepath.Join(root, "bin", "go"), "env", "GOVERSION")
	if err != nil {
		return ""
	}
	return out
}

func goStep() *Step {
	return &Step{
		ID: "go", Name: "Go", NeedsSudo: true, DefaultOn: true,
		Description: "Latest stable Go from go.dev, SHA256-verified, replacing the install root (per go.dev/doc/install).",
		Available: func() bool {
			return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
		},
		Version: func(ctx context.Context, cfg *config.Config) string {
			if v := installedGo(ctx, cfg.Go.Root); v != "" {
				return v
			}
			return "not installed"
		},
		Check: func(ctx context.Context, cfg *config.Config) CheckResult {
			latest, err := latestGo(ctx)
			if err != nil {
				return errResult(err)
			}
			cur := installedGo(ctx, cfg.Go.Root)
			if cur == latest.version {
				return upToDate()
			}
			if cur == "" {
				cur = "none"
			}
			return outdated(cur + " → " + latest.version)
		},
		Plan: func(ctx context.Context, cfg *config.Config) ([]Action, error) {
			root := cfg.Go.Root
			return []Action{{
				Label:   "install latest Go",
				Display: fmt.Sprintf("download go.dev tarball → verify sha256 → sudo rm -rf %s → sudo tar -C %s -xzf", root, filepath.Dir(root)),
				Run: func(ctx context.Context, out io.Writer) error {
					return updateGo(ctx, out, root)
				},
			}}, nil
		},
	}
}

func updateGo(ctx context.Context, out io.Writer, root string) error {
	latest, err := latestGo(ctx)
	if err != nil {
		return err
	}
	cur := installedGo(ctx, root)
	if cur == latest.version {
		fmt.Fprintf(out, "Go is up to date (%s).\n", cur)
		return nil
	}
	if cur == "" {
		cur = "none"
	}
	fmt.Fprintf(out, "Updating Go: %s -> %s\n", cur, latest.version)

	tmp, err := os.MkdirTemp("", "sysupdate-go-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, latest.file)
	if _, err := fetch.Download(ctx, out, "https://go.dev/dl/"+latest.file, file, latest.sha, 0); err != nil {
		fmt.Fprintln(out, "Download or checksum verification failed; existing install left untouched.")
		return err
	}
	fmt.Fprintln(out, "Checksum OK.")
	// Remove the old tree first; extracting over it can leave a broken install.
	if err := RunCmd(ctx, out, nil, "sudo", "-n", "rm", "-rf", root); err != nil {
		return fmt.Errorf("remove %s: %w", root, err)
	}
	if err := RunCmd(ctx, out, nil, "sudo", "-n", "tar", "-C", filepath.Dir(root), "-xzf", file); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	v, _ := Output(ctx, filepath.Join(root, "bin", "go"), "version")
	fmt.Fprintf(out, "Installed %s\n", v)
	return nil
}
