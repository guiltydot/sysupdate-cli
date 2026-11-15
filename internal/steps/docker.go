package steps

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/config"
)

// ---------- Docker images ----------

// dockerImage is a local image that was pulled from a registry.
type dockerImage struct {
	ref    string // repo:tag
	digest string // local repo digest, sha256:...
}

// pulledImages lists tagged images that have a registry digest. Images built
// locally (no digest) and untagged images are skipped: there is nothing to pull.
func pulledImages(ctx context.Context, cfg *config.Config) ([]dockerImage, error) {
	out, err := Output(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return nil, err
	}
	var list []dockerImage
	seen := map[string]bool{}
	for _, ref := range strings.Fields(out) {
		repo, tag, _ := strings.Cut(ref, ":")
		if seen[ref] || tag == "<none>" || repo == "<none>" || contains(cfg.Docker.Exclude, ref) || contains(cfg.Docker.Exclude, repo) {
			continue
		}
		seen[ref] = true
		digests, err := Output(ctx, "docker", "image", "inspect", "--format", "{{range .RepoDigests}}{{println .}}{{end}}", ref)
		if err != nil {
			continue
		}
		for _, d := range strings.Fields(digests) {
			if r, dg, ok := strings.Cut(d, "@"); ok && r == repo {
				list = append(list, dockerImage{ref, dg})
				break
			}
		}
	}
	return list, nil
}

func dockerAvailable() bool {
	if !Have("docker") {
		return false
	}
	_, err := Output(context.Background(), "docker", "info", "--format", "{{.ServerVersion}}")
	return err == nil
}

func dockerStep() *Step {
	return &Step{
		ID: "docker", Name: "Docker images", DefaultOn: false,
		Description: "docker pull for every pulled image that has a newer version in its registry. Locally built images are skipped. Off by default.",
		Available:   dockerAvailable,
		Version: func(ctx context.Context, cfg *config.Config) string {
			list, err := pulledImages(ctx, cfg)
			if err != nil {
				return ""
			}
			return plural(len(list), "image")
		},
		Check: withCleanup(func(ctx context.Context, cfg *config.Config) CheckResult {
			list, err := pulledImages(ctx, cfg)
			if err != nil {
				return errResult(err)
			}
			var (
				mu        sync.Mutex
				wg        sync.WaitGroup
				ups, errs []string
			)
			for _, img := range list {
				wg.Add(1)
				go func() {
					defer wg.Done()
					remote, err := OutputTimeout(ctx, 30*time.Second, "docker", "buildx", "imagetools", "inspect", "--format", "{{.Manifest.Digest}}", img.ref)
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err != nil:
						errs = append(errs, img.ref+": "+firstLine(err.Error()))
					case remote != img.digest:
						ups = append(ups, img.ref+" has a newer build")
					}
				}()
			}
			wg.Wait()
			if len(ups) == 0 && len(errs) == 0 {
				return upToDate()
			}
			if len(ups) == 0 {
				return CheckResult{State: CheckError, Summary: "registry check failed", Details: errs}
			}
			return outdated(plural(len(ups), "image"), append(ups, errs...)...)
		}, dockerCleanup),
		Plan: func(ctx context.Context, cfg *config.Config) ([]Action, error) {
			list, err := pulledImages(ctx, cfg)
			if err != nil {
				return nil, err
			}
			var acts []Action
			for _, img := range list {
				// docker pull only downloads layers when the registry has a newer build.
				acts = append(acts, Exec("pull "+img.ref, false, nil, "docker", "pull", img.ref))
			}
			return acts, nil
		},
	}
}

// dockerCleanup reports untagged images, which pulling a newer build leaves behind.
func dockerCleanup(ctx context.Context) []string {
	out, err := Output(ctx, "docker", "images", "--filter", "dangling=true", "--format", "{{.ID}} {{.Size}}")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] == "" {
		return nil
	}
	return []string{fmt.Sprintf("%s left untagged by newer pulls  (docker image prune)", plural(len(lines), "old image"))}
}

// ---------- gh extensions ----------

func ghExtStep() *Step {
	return &Step{
		ID: "gh-ext", Name: "gh extensions", DefaultOn: true,
		Description: "gh extension upgrade --all. gh itself is updated by the APT step (from GitHub's apt repository, if you added it).",
		Available:   func() bool { return Have("gh") },
		Version: func(ctx context.Context, cfg *config.Config) string {
			out, _ := Output(ctx, "gh", "extension", "list")
			n := 0
			for _, l := range strings.Split(out, "\n") {
				if strings.TrimSpace(l) != "" {
					n++
				}
			}
			return plural(n, "extension")
		},
		Check: func(ctx context.Context, _ *config.Config) CheckResult {
			list, _ := Output(ctx, "gh", "extension", "list")
			if strings.TrimSpace(list) == "" {
				return CheckResult{State: CheckUpToDate, Summary: "no extensions installed"}
			}
			out, err := CombinedOutput(ctx, "gh", "extension", "upgrade", "--all", "--dry-run")
			if err != nil && out == "" {
				return errResult(err)
			}
			var ups []string
			for _, l := range strings.Split(out, "\n") {
				l = strings.TrimSpace(l)
				if strings.Contains(l, "would have upgraded") || strings.Contains(l, " to ") && !strings.Contains(l, "already up to date") {
					ups = append(ups, l)
				}
			}
			if len(ups) == 0 {
				return upToDate()
			}
			return outdated(plural(len(ups), "extension"), ups...)
		},
		Plan: func(ctx context.Context, _ *config.Config) ([]Action, error) {
			return []Action{Exec("gh extension upgrade", false, nil, "gh", "extension", "upgrade", "--all")}, nil
		},
	}
}
