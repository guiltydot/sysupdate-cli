package vercmp

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"go1.27.4", "go1.27.4", 0},
		{"go1.27.10", "go1.27.4", 1},
		{"v26.13.1", "v26.2.0", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"0.72.0", "0.72", 0},
		{"2.1.325", "2.1.400", -1},
		{"v0.5.0", "v0.4.0", 1},
		{"v0.4.0", "v0.5.0", -1},
		{"v0.5.0-rc.2", "v0.5.0-rc.10", -1},
		{"v0.5.0-rc.1", "v0.5.0-rc", 1},
		{"v0.5.0-1", "v0.5.0-alpha", -1},
		{"v0.5.0-beta", "v0.5.0-alpha", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
