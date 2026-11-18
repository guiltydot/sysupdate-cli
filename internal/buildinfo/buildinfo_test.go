package buildinfo

import (
	"path/filepath"
	"testing"
)

func TestClassify(t *testing.T) {
	dir := t.TempDir()
	gobin := filepath.Join(dir, "go", "bin")
	local := filepath.Join(dir, ".local", "bin", "sysupdate")
	inGobin := filepath.Join(gobin, "sysupdate")
	cases := []struct {
		version, exe string
		want         Kind
	}{
		{"dev", local, Local},
		{"", local, Local},
		{"v0.0.0-dev", local, Local},
		{"v0.4.0-dirty", local, Local},
		{"v0.4.0-3-gabc1234", local, Local},
		{"v0.4.0-3-gabc1234-dirty", local, Local},
		{"v0.4.1-0.20261118163223-7fadc48c2af0", local, Local},
		{"v0.4.1-0.20261118163223-7fadc48c2af0+dirty", local, Local},
		{"v0.0.0-20261118163223-7fadc48c2af0", inGobin, Local},
		{"v0.4.0", local, Release},
		{"v0.5.0-rc.1", local, Release},
		// The release workflow and `go install @v0.4.0` give the same version;
		// only the location tells them apart.
		{"v0.4.0", inGobin, GoInstall},
	}
	for _, c := range cases {
		if got := classify(c.version, c.exe, []string{gobin}); got != c.want {
			t.Errorf("classify(%q, %q) = %v, want %v", c.version, c.exe, got, c.want)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		tag, current string
		want         bool
	}{
		{"v0.5.0", "v0.4.0", true},
		{"v0.4.0", "v0.4.0", false},
		{"v0.3.0", "v0.4.0", false}, // never downgrade
		{"v0.5.0", "v0.5.0-rc.1", true},
		{"v0.4.0", "v0.4.0-3-ge275482", false}, // describe builds are after their tag
		{"v0.4.0", "v0.4.0-3-ge275482-dirty", false},
		{"v0.5.0", "v0.4.0-3-ge275482", true},
		{"v0.4.0", "v0.4.0-dirty", false},
		{"v0.5.0", "v0.5.0-rc.1-2-gabc1234", true},
	}
	for _, c := range cases {
		if got := Newer(c.tag, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.tag, c.current, got, c.want)
		}
	}
}
