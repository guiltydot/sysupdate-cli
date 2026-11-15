package steps

import (
	"reflect"
	"testing"
)

func TestParseAptSim(t *testing.T) {
	out := `NOTE: This is only a simulation!
Reading package lists...
The following packages have been kept back:
  linux-generic linux-headers-generic
The following upgrades have been deferred due to phasing:
  python3-software-properties snapd
  software-properties-common
The following packages will be upgraded:
  curl libcurl4t64
2 upgraded, 0 newly installed, 0 to remove and 5 not upgraded.
Inst curl [8.5.0-2] (8.5.0-3 Ubuntu:26.04/resolute-updates [amd64])
Inst libcurl4t64 [8.5.0-2] (8.5.0-3 Ubuntu:26.04/resolute-updates [amd64]) []
Inst newpkg (1.0 Ubuntu:26.04/resolute [amd64])
Conf curl (8.5.0-3 Ubuntu:26.04/resolute-updates [amd64])
`
	pkgs, phased, kept := parseAptSim(out)
	wantPkgs := []string{"curl 8.5.0-2 → 8.5.0-3", "libcurl4t64 8.5.0-2 → 8.5.0-3", "newpkg 1.0 (new)"}
	if !reflect.DeepEqual(pkgs, wantPkgs) {
		t.Errorf("pkgs = %q, want %q", pkgs, wantPkgs)
	}
	if want := []string{"python3-software-properties", "snapd", "software-properties-common"}; !reflect.DeepEqual(phased, want) {
		t.Errorf("phased = %q, want %q", phased, want)
	}
	if want := []string{"linux-generic", "linux-headers-generic"}; !reflect.DeepEqual(kept, want) {
		t.Errorf("kept = %q, want %q", kept, want)
	}
}
