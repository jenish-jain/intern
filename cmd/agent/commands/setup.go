package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"intern/internal/config"
	"intern/internal/setup"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// SetupCmd walks through configuration interactively and writes .env.
var SetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactively configure intern and write .env",
	Long: `Asks only for the settings the chosen mode needs, keeps values from an
existing .env as defaults, writes .env (keeping the old one as .env.bak),
writes a Slack app manifest when Slack is used, and runs agent doctor.`,
	RunE: runSetup,
}

const manifestFile = "slack-app-manifest.yaml"

// SlackManifestCmd prints the Slack app manifest.
var SlackManifestCmd = &cobra.Command{
	Use:   "slack-manifest",
	Short: "Print the Slack app manifest with every scope and event intern uses",
	RunE: func(cmd *cobra.Command, args []string) error {
		u, _ := cmd.Flags().GetString("url")
		fmt.Fprint(cmd.OutOrStdout(), setup.SlackManifest(u))
		return nil
	},
}

func init() {
	SetupCmd.Flags().String("env-file", ".env", "file to write the configuration to")
	DoctorCmd.Flags().String("env-file", ".env", "configuration file to check")
	SlackManifestCmd.Flags().String("url", "", "public URL where intern runs, e.g. https://intern-xyz.a.run.app")
}

func runSetup(cmd *cobra.Command, args []string) error {
	envFile, _ := cmd.Flags().GetString("env-file")
	out := cmd.OutOrStdout()

	existing := map[string]string{}
	if _, err := os.Stat(envFile); err == nil {
		existing, err = godotenv.Read(envFile)
		if err != nil {
			return fmt.Errorf("read %s: %w", envFile, err)
		}
		fmt.Fprintf(out, "Found %s; its values are the defaults. Press enter to keep one.\n\n", envFile)
	}

	p := &setup.Prompter{In: bufio.NewReader(os.Stdin), Out: out}
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		p.ReadSecret = func() (string, error) {
			b, err := term.ReadPassword(fd)
			return string(b), err
		}
	}

	p.BeforeSection = func(s setup.Section) error {
		if s.Title != "Slack" || existing["SLACK_BOT_TOKEN"] != "" {
			return nil
		}
		if err := os.WriteFile(manifestFile, []byte(setup.SlackManifest(existing["PUBLIC_URL"])), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "  Create the Slack app first: at https://api.slack.com/apps choose Create New App >\n")
		fmt.Fprintf(out, "  From an app manifest, pick your workspace, paste %s (written just now),\n", manifestFile)
		fmt.Fprintf(out, "  then Install to Workspace. Events are added once intern has a public URL.\n")
		return nil
	}

	mode, err := p.ChooseMode(setup.ModeOf(existing))
	if err != nil {
		return err
	}
	values, err := p.Collect(mode, existing)
	if err != nil {
		return err
	}
	if mode.UsesSlack() {
		fmt.Fprintln(out, "\n== Slack app ==")
		u, err := p.Ask(setup.Field{Prompt: "Public URL where intern runs (empty if not deployed yet)", Optional: true,
			Help: "Slack sends events to <URL>/slack/events. The deploy script fills this in for Cloud Run."}, values["PUBLIC_URL"])
		if err != nil {
			return err
		}
		if u != "" {
			values["PUBLIC_URL"] = u
		}
	}
	if err := setup.WriteEnv(envFile, mode, values); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nWrote %s.\n", envFile)

	if mode.UsesSlack() {
		if err := os.WriteFile(manifestFile, []byte(setup.SlackManifest(values["PUBLIC_URL"])), 0o644); err != nil {
			return err
		}
		if values["PUBLIC_URL"] != "" {
			fmt.Fprintf(out, "Wrote %s with the events URL. Paste it into the app's App Manifest page\n", manifestFile)
			fmt.Fprintln(out, "(api.slack.com/apps > your app > App Manifest) once intern is running there.")
		}
		fmt.Fprintln(out, "Invite the bot to the channels it uses: /invite @intern")
	}

	if ok, err := p.Confirm("\nCheck the configuration now?", true); err != nil || !ok {
		printNextSteps(cmd, mode)
		return err
	}
	failed := runDoctor(cmd, envFile)
	printNextSteps(cmd, mode)
	if failed {
		return fmt.Errorf("some checks failed; fix them and run agent doctor")
	}
	return nil
}

func printNextSteps(cmd *cobra.Command, mode setup.Mode) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "\nNext:")
	fmt.Fprintf(out, "  Run locally:        %s\n", mode.Command())
	if mode.UsesSlack() {
		fmt.Fprintln(out, "  Deploy to Cloud Run: scripts/deploy-cloudrun.sh   (see docs/SETUP.md)")
	}
	if mode == setup.ModeTeam {
		fmt.Fprintln(out, "  Then in Slack:      @intern plan: <your PRD>")
	}
}

// DoctorCmd checks the configuration against the real services.
var DoctorCmd = &cobra.Command{
	Use:          "doctor",
	Short:        "Check the configuration: credentials, repo, JIRA workflow, Slack and tools",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		envFile, _ := cmd.Flags().GetString("env-file")
		if runDoctor(cmd, envFile) {
			return fmt.Errorf("some checks failed")
		}
		return nil
	},
}

// runDoctor prints each check and reports whether any failed. A check that
// isn't OK lists the settings it used, their values (secrets masked) and
// whether each came from envFile or the shell.
func runDoctor(cmd *cobra.Command, envFile string) bool {
	out := cmd.OutOrStdout()
	vals := setup.Values{EnvFile: envFile, Shell: setup.CaptureShell(), File: map[string]string{}}
	if f, err := godotenv.Read(envFile); err == nil {
		vals.File = f
		// Like the agent: the shell's values win over the file.
		_ = godotenv.Load(envFile)
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(out, "✗ Configuration: can't read %s: %v\n", envFile, err)
		return true
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(out, "✗ Configuration: %v\n    Fix: run `agent setup`, or set the variable in %s.\n", err, envFile)
		return true
	}
	fmt.Fprintln(out, "✓ Configuration: valid")
	failed := false
	for _, r := range (&setup.Doctor{Cfg: cfg}).Run(context.Background()) {
		mark := "✓"
		switch r.Status {
		case setup.StatusWarn:
			mark = "!"
		case setup.StatusFail:
			mark = "✗"
			failed = true
		}
		fmt.Fprintf(out, "%s %s: %s\n", mark, r.Name, r.Detail)
		if r.Status == setup.StatusOK {
			continue
		}
		for _, k := range r.Keys {
			fmt.Fprintf(out, "    Using: %s\n", vals.Describe(k))
		}
		if r.Fix != "" {
			fmt.Fprintf(out, "    Fix: %s\n", r.Fix)
		}
	}
	return failed
}
