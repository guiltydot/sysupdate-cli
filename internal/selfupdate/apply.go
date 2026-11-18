package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/guiltydot/sysupdate-cli/internal/fetch"
)

const repo = "guiltydot/sysupdate-cli"

var (
	// Executable is replaced in tests.
	Executable = os.Executable
	// Limits on what is downloaded and extracted; variables so tests can lower them.
	maxArchiveSize int64 = 128 << 20
	maxBinarySize  int64 = 64 << 20
	// downloadTimeout bounds the archive download.
	downloadTimeout = 10 * time.Minute
)

// Target returns the path of the running binary with symlinks resolved; that
// is the file Apply replaces.
func Target() (string, error) {
	target, err := Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(target)
}

// CheckWritable returns an error, with advice, if the directory holding target
// cannot be written, so the binary cannot be replaced without root.
func CheckWritable(target string) error {
	dir := filepath.Dir(target)
	if err := syscall.Access(dir, 0x2 /* W_OK */); err != nil {
		return fmt.Errorf("%s is not writable (%w): reinstall sysupdate to ~/.local/bin or run sudo sysupdate self-update", dir, err)
	}
	return nil
}

// AssetFor returns the release archive for this machine's architecture.
func AssetFor(release Release) (Asset, error) {
	name := fmt.Sprintf("sysupdate_%s_linux_%s.tar.gz", release.TagName, runtime.GOARCH)
	if asset, ok := findAsset(release, name); ok {
		return asset, nil
	}
	return Asset{}, fmt.Errorf("no asset for linux/%s (expected %s)", runtime.GOARCH, name)
}

// ParseChecksums parses sha256sum output; a leading "./" in names is dropped.
func ParseChecksums(text string) (map[string]string, error) {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid checksum line %q", line)
		}
		sum, name := strings.ToLower(fields[0]), strings.TrimPrefix(fields[1], "./")
		if len(sum) != 64 {
			return nil, fmt.Errorf("invalid sha256 in line %q", line)
		}
		out[name] = sum
	}
	return out, nil
}

func findAsset(release Release, name string) (Asset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return Asset{}, false
}

// Apply replaces the running binary with the one from release. The old binary
// is only touched by the final rename, after the download, checksum,
// attestation (when gh can check it) and a smoke test of the new binary passed.
func Apply(ctx context.Context, out io.Writer, release Release) error {
	if out == nil {
		out = io.Discard
	}
	target, err := Target()
	if err != nil {
		return err
	}
	if err := CheckWritable(target); err != nil {
		return err
	}

	archiveAsset, err := AssetFor(release)
	if err != nil {
		return err
	}
	checksumAsset, ok := findAsset(release, "checksums.txt")
	if !ok {
		return errors.New("release has no checksums.txt asset")
	}
	checksumsText, err := fetchText(ctx, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	checksums, err := ParseChecksums(checksumsText)
	if err != nil {
		return err
	}
	want, ok := checksums[archiveAsset.Name]
	if !ok {
		return fmt.Errorf("checksums.txt has no line for %s", archiveAsset.Name)
	}

	tmpDir, err := os.MkdirTemp("", "sysupdate-self-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, archiveAsset.Name)
	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	n, err := fetch.Download(dlCtx, out, archiveAsset.BrowserDownloadURL, archivePath, want, maxArchiveSize)
	if err != nil {
		return err
	}
	if archiveAsset.Size > 0 && n != archiveAsset.Size {
		return fmt.Errorf("archive size mismatch: got %d bytes, want %d", n, archiveAsset.Size)
	}
	fmt.Fprintln(out, "Checksum OK.")

	if err := verifyAttestation(ctx, out, archivePath); err != nil {
		return err
	}

	// The new binary goes next to the target, so the rename stays on one filesystem.
	tmpBin, err := extractBinary(filepath.Dir(target), archivePath, release.TagName)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpBin) }() // no-op once renamed

	smokeCtx, cancelSmoke := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSmoke()
	smoke, err := exec.CommandContext(smokeCtx, tmpBin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("smoke test of the new binary failed: %w: %s", err, strings.TrimSpace(string(smoke)))
	}
	if !strings.Contains(string(smoke), release.TagName) {
		return fmt.Errorf("smoke test output did not contain %s: %s", release.TagName, strings.TrimSpace(string(smoke)))
	}
	return os.Rename(tmpBin, target)
}

// verifyAttestation checks the archive's build provenance with gh when gh is
// installed and logged in (gh needs a login to fetch attestations). A failed
// verification is an error; a missing or logged-out gh only leaves a note.
func verifyAttestation(ctx context.Context, out io.Writer, archive string) error {
	gh, err := exec.LookPath("gh")
	if err != nil {
		fmt.Fprintln(out, "gh not found: only the release checksum was checked.")
		return nil
	}
	output, err := exec.CommandContext(ctx, gh, "attestation", "verify", archive, "--repo", repo).CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 4 { // gh: authentication required
		fmt.Fprintln(out, "gh is not logged in: only the release checksum was checked (run gh auth login to also verify the build attestation).")
		return nil
	}
	if err != nil {
		return fmt.Errorf("gh attestation verify failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	fmt.Fprintln(out, "Attestation OK.")
	return nil
}

// extractBinary writes sysupdate_<tag>_linux_<arch>/sysupdate from the archive
// to a new executable temp file in dir and returns its path.
func extractBinary(dir, archivePath, tag string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	want := fmt.Sprintf("sysupdate_%s_linux_%s/sysupdate", tag, runtime.GOARCH)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("archive has no binary entry %s", want)
		}
		if err != nil {
			return "", err
		}
		if h.Name != want {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("archive entry %s is not a regular file", h.Name)
		}
		if h.Size > maxBinarySize {
			return "", fmt.Errorf("archive entry %s is %d bytes, more than the %d allowed", h.Name, h.Size, maxBinarySize)
		}
		tmp, err := os.CreateTemp(dir, ".sysupdate-new-*")
		if err != nil {
			return "", err
		}
		name := tmp.Name()
		_, err = io.Copy(tmp, io.LimitReader(tr, h.Size))
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(name, 0o755) //nolint:gosec // the new binary must be executable
		}
		if err != nil {
			_ = os.Remove(name)
			return "", err
		}
		return name, nil
	}
}
