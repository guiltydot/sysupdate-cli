// Package vercmp compares dotted numeric versions such as "v1.2.3", "go1.25.1"
// and "1.2.3-rc.1".
package vercmp

import (
	"strconv"
	"strings"
)

// Compare returns -1, 0 or 1. Missing components count as 0, so "1.2" equals
// "1.2.0". A prerelease sorts before its release, and prerelease identifiers
// are compared as in SemVer: numeric ones numerically, so rc.2 < rc.10.
func Compare(a, b string) int {
	pa, preA := split(a)
	pb, preB := split(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return cmpInt(x, y)
		}
	}
	switch {
	case preA == preB:
		return 0
	case preA == "":
		return 1
	case preB == "":
		return -1
	}
	return comparePre(preA, preB)
}

// IsPrerelease reports whether v has a prerelease suffix ("-rc1").
func IsPrerelease(v string) bool {
	_, pre := split(v)
	return pre != ""
}

func split(v string) ([]int, string) {
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

func comparePre(a, b string) int {
	ia, ib := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(ia) && i < len(ib); i++ {
		na, errA := strconv.Atoi(ia[i])
		nb, errB := strconv.Atoi(ib[i])
		switch {
		case errA == nil && errB == nil:
			if na != nb {
				return cmpInt(na, nb)
			}
		case errA == nil: // numeric identifiers sort before alphanumeric ones
			return -1
		case errB == nil:
			return 1
		default:
			if c := strings.Compare(ia[i], ib[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(ia), len(ib))
}

func cmpInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
