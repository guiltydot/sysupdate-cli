package steps

import (
	"strconv"
	"strings"
)

// compareVersions compares dotted numeric versions ("v1.2.3", "go1.25.1", "1.2.3-rc1").
// Prerelease suffixes sort before the release. Returns -1, 0 or 1.
func compareVersions(a, b string) int {
	pa, preA := splitVersion(a)
	pb, preB := splitVersion(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case preA == preB:
		return 0
	case preA == "":
		return 1
	case preB == "":
		return -1
	case preA < preB:
		return -1
	default:
		return 1
	}
}

func splitVersion(v string) ([]int, string) {
	v = strings.TrimLeft(v, "vgo")
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	var nums []int
	for _, part := range strings.Split(core, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		nums = append(nums, n)
	}
	return nums, pre
}

func isPrerelease(v string) bool {
	_, pre := splitVersion(v)
	return pre != ""
}
