package orchestrator

import "testing"

func TestBuildBranchName(t *testing.T) {
	cases := []struct {
		name      string
		prefix    string
		ticketKey string
		want      string
	}{
		{
			name:      "empty prefix does not produce a leading slash",
			prefix:    "",
			ticketKey: "SLACK-C0AS8FQ2FB4-1785575200.144769",
			want:      "slack-c0as8fq2fb4-1785575200-1",
		},
		{
			name:      "prefix is joined with a slash",
			prefix:    "feature",
			ticketKey: "PROJ-123",
			want:      "feature/proj-123",
		},
		{
			name:      "prefix with trailing slash is not doubled",
			prefix:    "feature/",
			ticketKey: "PROJ-123",
			want:      "feature/proj-123",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildBranchName(tc.prefix, tc.ticketKey)
			if got == "" || got[0] == '/' {
				t.Fatalf("buildBranchName(%q, %q) = %q, invalid git ref name", tc.prefix, tc.ticketKey, got)
			}
			if got != tc.want {
				t.Errorf("buildBranchName(%q, %q) = %q, want %q", tc.prefix, tc.ticketKey, got, tc.want)
			}
		})
	}
}
