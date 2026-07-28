package fetch

import "testing"

// The production allowlist moved here from cmd/eventsintel/main.go so that
// eventscout's promotion output can diff candidate hosts against the same
// list the ingest command actually uses. These checks pin the invariants the
// move must preserve: the known venues stay present and nothing is listed
// twice.
func TestProductionAllowedHostsContainsKnownVenues(t *testing.T) {
	for _, host := range []string{"www.coex.co.kr", "www.kintex.com", "www.ces.tech", "neurips.cc", "k-ai.or.kr"} {
		found := false
		for _, h := range ProductionAllowedHosts {
			if h == host {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ProductionAllowedHosts missing %q", host)
		}
	}
}

func TestProductionAllowedHostsHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(ProductionAllowedHosts))
	for _, h := range ProductionAllowedHosts {
		if seen[h] {
			t.Errorf("duplicate host %q", h)
		}
		seen[h] = true
	}
}
