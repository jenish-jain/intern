package setup

import (
	"fmt"
	"os"
	"strings"
)

// Values explains where each setting came from. The agent loads .env
// without overriding variables already set in the shell, so a stale export
// can silently win over a corrected .env.
type Values struct {
	EnvFile string
	File    map[string]string // values in the .env file
	Shell   map[string]string // values set before .env was loaded
}

// CaptureShell records the environment before .env is loaded.
func CaptureShell() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

// Describe shows the value a check used for key, masked when it's a
// secret, and where it came from.
func (v Values) Describe(key string) string {
	shell, inShell := v.Shell[key]
	file, inFile := v.File[key]
	switch {
	case inShell && inFile && shell != file:
		return fmt.Sprintf("%s=%s from your shell, which overrides %s=%s in %s; run `unset %s`",
			key, display(key, shell), key, display(key, file), v.EnvFile, key)
	case inShell:
		return fmt.Sprintf("%s=%s from your shell", key, display(key, shell))
	case inFile:
		return fmt.Sprintf("%s=%s in %s", key, display(key, file), v.EnvFile)
	}
	return key + " is not set"
}

// IsSecret reports whether a setting's value must not be printed.
func IsSecret(key string) bool {
	for _, s := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD"} {
		if strings.Contains(key, s) && key != "JIRA_PROJECT_KEY" {
			return true
		}
	}
	return false
}

func display(key, val string) string {
	if val == "" {
		return `""`
	}
	if !IsSecret(key) {
		return val
	}
	return fmt.Sprintf("%s (%d characters)", mask(val), len(val))
}
