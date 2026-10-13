package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Prompter asks questions on a terminal.
type Prompter struct {
	In  *bufio.Reader
	Out io.Writer
	// ReadSecret reads a line without echoing it. Nil reads a plain line.
	ReadSecret func() (string, error)
	// BeforeSection runs before a section's questions, e.g. to explain
	// how to get the values.
	BeforeSection func(Section) error
}

// ChooseMode asks which mode to set up. current preselects a mode when an
// existing configuration implies one.
func (p *Prompter) ChooseMode(current Mode) (Mode, error) {
	fmt.Fprintln(p.Out, "How should intern get its work?")
	def := 1
	for i, m := range Modes {
		fmt.Fprintf(p.Out, "  %d) %s\n", i+1, m.Summary)
		if m.Mode == current {
			def = i + 1
		}
	}
	for {
		fmt.Fprintf(p.Out, "Choose 1-%d [%d]: ", len(Modes), def)
		line, err := p.readLine()
		if err != nil {
			return "", err
		}
		if line == "" {
			return Modes[def-1].Mode, nil
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(Modes) {
			return Modes[n-1].Mode, nil
		}
		fmt.Fprintln(p.Out, "  Please enter a number from the list.")
	}
}

// Ask prompts for one field. current is the value already configured, if
// any; pressing enter keeps it, or takes the field's default.
func (p *Prompter) Ask(f Field, current string) (string, error) {
	if f.Help != "" {
		fmt.Fprintf(p.Out, "  %s\n", f.Help)
	}
	def := f.Default
	if current != "" {
		def = current
	}
	for {
		switch {
		case f.Secret && def != "":
			fmt.Fprintf(p.Out, "%s [keep current %s]: ", f.Prompt, mask(def))
		case def != "":
			fmt.Fprintf(p.Out, "%s [%s]: ", f.Prompt, def)
		case f.Optional:
			fmt.Fprintf(p.Out, "%s (optional): ", f.Prompt)
		default:
			fmt.Fprintf(p.Out, "%s: ", f.Prompt)
		}
		var line string
		var err error
		if f.Secret && p.ReadSecret != nil {
			line, err = p.ReadSecret()
			fmt.Fprintln(p.Out)
			line = strings.TrimSpace(line)
		} else {
			line, err = p.readLine()
		}
		if err != nil {
			return "", err
		}
		if line == "" {
			line = def
		}
		if line == "" && !f.Optional {
			fmt.Fprintln(p.Out, "  A value is required.")
			continue
		}
		if line != "" && f.Check != nil {
			if err := f.Check(line); err != nil {
				fmt.Fprintf(p.Out, "  %v\n", err)
				continue
			}
		}
		return line, nil
	}
}

// Confirm asks a yes/no question.
func (p *Prompter) Confirm(question string, def bool) (bool, error) {
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	fmt.Fprintf(p.Out, "%s [%s]: ", question, hint)
	line, err := p.readLine()
	if err != nil {
		return false, err
	}
	switch strings.ToLower(line) {
	case "":
		return def, nil
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func (p *Prompter) readLine() (string, error) {
	line, err := p.In.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// Collect runs the questions for a mode and returns every setting to write:
// the mode's fixed settings plus the answers, on top of existing.
func (p *Prompter) Collect(m Mode, existing map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(existing))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range Fixed(m) {
		out[k] = v
	}
	for _, s := range Sections(m) {
		fmt.Fprintf(p.Out, "\n== %s ==\n", s.Title)
		if p.BeforeSection != nil {
			if err := p.BeforeSection(s); err != nil {
				return nil, err
			}
		}
		for _, f := range s.Fields {
			v, err := p.Ask(f, existing[f.Key])
			if err != nil {
				return nil, err
			}
			if v == "" {
				delete(out, f.Key)
			} else {
				out[f.Key] = v
			}
		}
	}
	return out, nil
}

// ModeOf infers the mode an existing configuration was written for.
func ModeOf(env map[string]string) Mode {
	switch {
	case env["TICKETING_MODE"] == "slack":
		return ModeSlack
	case env["TICKETING_MODE"] == "jira" && env["SLACK_BOT_TOKEN"] != "":
		return ModeTeam
	case env["TICKETING_MODE"] == "jira" || env["JIRA_URL"] != "":
		return ModeJira
	}
	return ModeTeam
}

// RenderEnv formats settings as a .env file, grouped by the mode's
// sections. Settings outside those sections are kept under "Other".
func RenderEnv(m Mode, values map[string]string) string {
	var b strings.Builder
	b.WriteString("# Written by `agent setup`. Re-run it to change these, or edit by hand.\n")
	b.WriteString("# Check the configuration with `agent doctor`.\n")
	written := map[string]bool{}
	writeKV := func(k string) {
		if v, ok := values[k]; ok && !written[k] {
			fmt.Fprintf(&b, "%s=%s\n", k, quote(v))
			written[k] = true
		}
	}

	b.WriteString("\n# Mode\n")
	fixed := Fixed(m)
	for _, k := range sortedKeys(fixed) {
		writeKV(k)
	}
	for _, s := range Sections(m) {
		fmt.Fprintf(&b, "\n# %s\n", s.Title)
		for _, f := range s.Fields {
			writeKV(f.Key)
		}
	}
	var rest []string
	for k := range values {
		if !written[k] {
			rest = append(rest, k)
		}
	}
	if len(rest) > 0 {
		sort.Strings(rest)
		b.WriteString("\n# Other\n")
		for _, k := range rest {
			writeKV(k)
		}
	}
	return b.String()
}

// WriteEnv writes the settings to path, keeping the previous file as
// path.bak.
func WriteEnv(path string, m Mode, values map[string]string) error {
	if old, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
	}
	return os.WriteFile(path, []byte(RenderEnv(m, values)), 0o600)
}

// quote makes a value safe for both godotenv and a shell that sources the
// file: single quotes stop variable expansion in each.
func quote(v string) string {
	if v != "" && !strings.ContainsAny(v, " #\"'$\\`") {
		return v
	}
	if !strings.Contains(v, "'") {
		return "'" + v + "'"
	}
	return strconv.Quote(v)
}

func mask(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
