package policy

import "testing"

func TestDefaultSnapshotMatchesWorkSubdomains(t *testing.T) {
	rule, ok := DefaultSnapshot().Match("api.github.com")
	if !ok || rule.ID != "github" || rule.Priority != "high" {
		t.Fatalf("expected GitHub high-priority match, got %#v, %t", rule, ok)
	}
}