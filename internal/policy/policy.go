package policy

import "strings"

type Rule struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Priority string   `json:"priority"`
	Domains  []string `json:"domains"`
}

type Snapshot struct {
	Version string `json:"version"`
	Rules   []Rule `json:"rules"`
}

func DefaultSnapshot() Snapshot {
	return Snapshot{
		Version: "2026-10-19.1",
		Rules: []Rule{
			{ID: "zoom", Name: "Zoom", Priority: "high", Domains: []string{"zoom.us", "zoom.com"}},
			{ID: "slack", Name: "Slack", Priority: "high", Domains: []string{"slack.com", "slack-edge.com"}},
			{ID: "teams", Name: "Microsoft Teams", Priority: "high", Domains: []string{"teams.microsoft.com", "teams.live.com"}},
			{ID: "github", Name: "GitHub", Priority: "high", Domains: []string{"github.com", "githubusercontent.com"}},
			{ID: "bitbucket", Name: "Bitbucket", Priority: "high", Domains: []string{"bitbucket.org", "atlassian.net"}},
			{ID: "productivity", Name: "Productivity suite", Priority: "high", Domains: []string{"office.com", "office365.com", "microsoftonline.com", "google.com", "googleusercontent.com", "notion.so", "figma.com", "asana.com"}},
		},
	}
}

func (s Snapshot) Match(hostname string) (Rule, bool) {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	for _, rule := range s.Rules {
		for _, domain := range rule.Domains {
			if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
				return rule, true
			}
		}
	}
	return Rule{}, false
}