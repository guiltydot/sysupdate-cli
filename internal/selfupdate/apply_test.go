package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testTag = "v9.9.9"

var goodBinary = []byte("#!/bin/sh\nprintf 'sysupdate " + testTag + "\\n'\n")

func makeTar(t *testing.T, entry string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	h := &tar.Header{Name: entry, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fixture struct {
	entry     string // archive entry name; default is the expected layout
	body      []byte // binary in the archive; default goodBinary
	checksums func(archiveName, sum string) string
	size      func(archive []byte) int64 // asset metadata size
	gh        string                     // body of a fake gh script; "" means gh is not on PATH
}

// setup builds a release served by httptest and a target binary containing
// "old" in a temp dir, and returns the release and the target path.
func setup(t *testing.T, fx fixture) (Release, string) {
	t.Helper()
	if fx.entry == "" {
		fx.entry = fmt.Sprintf("sysupdate_%s_linux_%s/sysupdate", testTag, runtime.GOARCH)
	}
	if fx.body == nil {
		fx.body = goodBinary
	}
	archiveName := fmt.Sprintf("sysupdate_%s_linux_%s.tar.gz", testTag, runtime.GOARCH)
	archive := makeTar(t, fx.entry, fx.body)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  ./%s\n", hex.EncodeToString(sum[:]), archiveName)
	if fx.checksums != nil {
		checksums = fx.checksums(archiveName, hex.EncodeToString(sum[:]))
	}
	size := int64(len(archive))
	if fx.size != nil {
		size = fx.size(archive)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + archiveName:
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, "sysupdate")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	oldExecutable := Executable
	Executable = func() (string, error) { return target, nil }
	t.Cleanup(func() { Executable = oldExecutable })

	binDir := t.TempDir()
	if fx.gh != "" {
		if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte("#!/bin/sh\n"+fx.gh+"\n"), 0o755); err != nil { //nolint:gosec // fake gh
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)

	return Release{TagName: testTag, Assets: []Asset{
		{Name: archiveName, BrowserDownloadURL: server.URL + "/" + archiveName, Size: size},
		{Name: "checksums.txt", BrowserDownloadURL: server.URL + "/checksums.txt"},
	}}, target
}

func readTarget(t *testing.T, target string) string {
	t.Helper()
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// leftovers lists the temp files Apply left next to the target.
func leftovers(t *testing.T, target string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".sysupdate-*"))
	return m
}

func TestParseChecksums(t *testing.T) {
	if _, err := ParseChecksums("abc  ./sysupdate_v1.0.0_linux_amd64.tar.gz\n"); err == nil {
		t.Fatal("expected a short sha256 to fail")
	}
	valid := strings.Repeat("a", 64)
	for _, name := range []string{"./sysupdate_v1.0.0_linux_amd64.tar.gz", "sysupdate_v1.0.0_linux_amd64.tar.gz"} {
		got, err := ParseChecksums(valid + "  " + name + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if got["sysupdate_v1.0.0_linux_amd64.tar.gz"] != valid {
			t.Fatalf("%s: unexpected parsed checksum: %#v", name, got)
		}
	}
}

func TestApplyHappyPath(t *testing.T) {
	release, target := setup(t, fixture{})
	var out bytes.Buffer
	if err := Apply(context.Background(), &out, release); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != string(goodBinary) {
		t.Fatalf("target was not replaced: %q", got)
	}
	if !strings.Contains(out.String(), "gh not found") {
		t.Errorf("missing note about gh: %q", out.String())
	}
	if l := leftovers(t, target); len(l) != 0 {
		t.Errorf("temp files left behind: %v", l)
	}
}

func TestApplyGhNotLoggedIn(t *testing.T) {
	release, target := setup(t, fixture{gh: "exit 4"})
	var out bytes.Buffer
	if err := Apply(context.Background(), &out, release); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != string(goodBinary) {
		t.Fatalf("target was not replaced: %q", got)
	}
	if !strings.Contains(out.String(), "not logged in") {
		t.Errorf("missing note about gh login: %q", out.String())
	}
}

func TestApplyGhVerifies(t *testing.T) {
	release, target := setup(t, fixture{gh: `[ "$1 $2" = "attestation verify" ] || exit 9`})
	var out bytes.Buffer
	if err := Apply(context.Background(), &out, release); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != string(goodBinary) {
		t.Fatalf("target was not replaced: %q", got)
	}
	if !strings.Contains(out.String(), "Attestation OK") {
		t.Errorf("attestation not reported: %q", out.String())
	}
}

func TestApplyFailuresLeaveTargetUnchanged(t *testing.T) {
	oldMax := maxBinarySize
	t.Cleanup(func() { maxBinarySize = oldMax })

	cases := []struct {
		name string
		fx   fixture
		max  int64
	}{
		{name: "checksum mismatch", fx: fixture{checksums: func(a, _ string) string {
			return strings.Repeat("0", 64) + "  ./" + a + "\n"
		}}},
		{name: "missing checksum line", fx: fixture{checksums: func(_, sum string) string {
			return sum + "  ./another.tar.gz\n"
		}}},
		{name: "size mismatch", fx: fixture{size: func(a []byte) int64 { return int64(len(a)) + 1 }}},
		{name: "wrong layout", fx: fixture{entry: "wrong/sysupdate"}},
		{name: "oversized entry", fx: fixture{}, max: 8},
		{name: "smoke test fails", fx: fixture{body: []byte("#!/bin/sh\nexit 1\n")}},
		{name: "smoke test prints another version", fx: fixture{body: []byte("#!/bin/sh\necho sysupdate v1.0.0\n")}},
		{name: "attestation fails", fx: fixture{gh: "echo no attestation; exit 1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			maxBinarySize = oldMax
			if tc.max > 0 {
				maxBinarySize = tc.max
			}
			release, target := setup(t, tc.fx)
			err := Apply(context.Background(), nil, release)
			if err == nil {
				t.Fatal("Apply unexpectedly succeeded")
			}
			t.Log(err)
			if got := readTarget(t, target); got != "old" {
				t.Fatalf("target changed: %q", got)
			}
			if l := leftovers(t, target); len(l) != 0 {
				t.Errorf("temp files left behind: %v", l)
			}
		})
	}
}

func TestApplyUnwritableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	release, target := setup(t, fixture{})
	dir := filepath.Dir(target)
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // test dir
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // let t.TempDir clean up

	err := Apply(context.Background(), nil, release)
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("want a not-writable error, got %v", err)
	}
	if got := readTarget(t, target); got != "old" {
		t.Fatalf("target changed: %q", got)
	}
}
