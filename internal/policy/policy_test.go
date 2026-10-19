package policy

import "testing"

// TestDefaultSnapshotMatchesWorkSubdomains verifies inherited policy for SaaS subdomains.
// It uses a GitHub API hostname rather than the exact configured parent domain.
// The assertions require both successful matching and the expected high-priority rule.
// This protects the suffix-matching contract used by the data-plane decision agent.
func TestDefaultSnapshotMatchesWorkSubdomains(t *testing.T) {
	rule, ok := DefaultSnapshot().Match("api.github.com")
	if !ok || rule.ID != "github" || rule.Priority != "high" {
		t.Fatalf("expected GitHub high-priority match, got %#v, %t", rule, ok)
	}
}
