package sandbox

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocal_ScrubsCredentials(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("JIRA_API_TOKEN", "jira_secret")
	t.Setenv("INTERN_TEST_PASS", "visible")
	var out bytes.Buffer
	l := &Local{PassEnv: []string{"INTERN_TEST_PASS"}}
	err := l.Run(context.Background(), Cmd{Dir: t.TempDir(), Args: []string{"env"}, Env: []string{"EXTRA=1"}, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	env := out.String()
	for _, secret := range []string{"ghp_secret", "jira_secret"} {
		if strings.Contains(env, secret) {
			t.Errorf("credential leaked into sandbox env: %s", secret)
		}
	}
	for _, want := range []string{"INTERN_TEST_PASS=visible", "EXTRA=1", "PATH="} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
	if l.Isolated() {
		t.Error("local sandbox must not claim isolation")
	}
}

func TestLocal_TimeoutKillsChildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	var out bytes.Buffer
	err := (&Local{}).Run(ctx, Cmd{Dir: t.TempDir(), Args: []string{"sh", "-c", "sleep 5; echo late"}, Stdout: &out})
	if err == nil {
		t.Fatal("expected error on timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("child process outlived the timeout")
	}
}

func TestDocker_RunArgs(t *testing.T) {
	d := &Docker{Image: "intern-sandbox:latest", Network: "none", ExtraArgs: []string{"--memory=4g"}}
	args, err := d.runArgs(Cmd{Dir: "/tmp/ws", Args: []string{"claude", "-p", "hi"}, Env: []string{"ANTHROPIC_API_KEY=k"}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"run --rm -i", "--network none", "-v /tmp/ws:/workspace", "-w /workspace",
		"-e ANTHROPIC_API_KEY=k", "--memory=4g", "intern-sandbox:latest claude -p hi", "--user ",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("docker args missing %q: %s", want, joined)
		}
	}
	if !d.Isolated() {
		t.Error("docker sandbox should be isolated")
	}
	if _, err := (&Docker{}).runArgs(Cmd{Args: []string{"x"}}); err == nil {
		t.Error("expected error without image")
	}
	if _, err := d.runArgs(Cmd{Args: []string{"x"}, Env: []string{"NOEQUALS"}}); err == nil {
		t.Error("expected error for malformed env")
	}
}

// TestDocker_Real runs a command in a real container. It needs Docker and
// an image: INTERN_E2E_DOCKER_IMAGE=intern-sandbox:latest. Set
// INTERN_E2E_DOCKER_DIR to a directory the Docker VM shares if temp dirs
// aren't shared.
func TestDocker_Real(t *testing.T) {
	image := os.Getenv("INTERN_E2E_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set INTERN_E2E_DOCKER_IMAGE to run against real Docker")
	}
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	// Docker VMs (Colima, Docker Desktop) only share some host paths, often
	// just $HOME, so the workspace may need to live under a shared base.
	dir := t.TempDir()
	if base := os.Getenv("INTERN_E2E_DOCKER_DIR"); base != "" {
		var err error
		if dir, err = os.MkdirTemp(base, "sandbox-test-"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	var out bytes.Buffer
	d := &Docker{Image: image, Network: "none"}
	err := d.Run(context.Background(), Cmd{
		Dir:    dir,
		Args:   []string{"sh", "-c", "echo hi > made.txt && env"},
		Env:    []string{"VISIBLE=1"},
		Stdout: &out,
		Stderr: &out,
	})
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "ghp_secret") {
		t.Error("host credential leaked into container")
	}
	if !strings.Contains(out.String(), "VISIBLE=1") {
		t.Error("job env not passed")
	}
	info, err := os.Stat(filepath.Join(dir, "made.txt"))
	if err != nil {
		t.Fatalf("file written in container not visible on host: %v", err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		t.Errorf("file owned by uid %d, want host uid %d", st.Uid, os.Getuid())
	}
}
