package config

import (
	"strings"
	"testing"
)

func TestParseModelRouting(t *testing.T) {
	r, err := ParseModelRouting("")
	if err != nil || r["developer"] != [3]string{"haiku", "sonnet", "opus"} || r["architect"][0] != "opus" || r["reviewer"][0] != "haiku" {
		t.Fatalf("defaults wrong: %v %v", r, err)
	}
	r, err = ParseModelRouting(" developer:S=haiku , reviewer=opus,tester:l=claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}
	if r["developer"] != [3]string{"haiku", "sonnet", "opus"} {
		t.Errorf("developer = %v", r["developer"])
	}
	if r["reviewer"] != [3]string{"opus", "opus", "opus"} {
		t.Errorf("reviewer = %v", r["reviewer"])
	}
	if r["tester"][2] != "claude-opus-5-5" {
		t.Errorf("tester L = %q", r["tester"][2])
	}
	for spec, want := range map[string]string{
		"designer=opus":     "unknown role",
		"developer:XL=opus": "unknown tier",
		"developer":         "want role=model",
		"developer=":        "want role=model",
	} {
		if _, err := ParseModelRouting(spec); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseModelRouting(%q) error = %v, want %q", spec, err, want)
		}
	}
}
