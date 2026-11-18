package steps

import "github.com/guiltydot/sysupdate-cli/internal/vercmp"

// compareVersions compares dotted numeric versions ("v1.2.3", "go1.25.1", "1.2.3-rc1").
// Prerelease suffixes sort before the release. Returns -1, 0 or 1.
func compareVersions(a, b string) int { return vercmp.Compare(a, b) }

func isPrerelease(v string) bool { return vercmp.IsPrerelease(v) }
