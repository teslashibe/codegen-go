package codegen

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestBuildCodexArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rc        runConfig
		streaming bool
		want      []string
	}{
		{
			name: "defaults: no model, no sandbox",
			rc:   runConfig{},
			want: []string{"exec", "-s", "workspace-write", "--skip-git-repo-check", "-"},
		},
		{
			name: "json output format includes --json before sandbox",
			rc:   runConfig{outputFormat: "json"},
			want: []string{"exec", "--json", "-s", "workspace-write", "--skip-git-repo-check", "-"},
		},
		{
			name:      "streaming forces --json",
			rc:        runConfig{},
			streaming: true,
			want:      []string{"exec", "--json", "-s", "workspace-write", "--skip-git-repo-check", "-"},
		},
		{
			name:      "streaming forces --json even when outputFormat is text",
			rc:        runConfig{outputFormat: "text"},
			streaming: true,
			want:      []string{"exec", "--json", "-s", "workspace-write", "--skip-git-repo-check", "-"},
		},
		{
			name: "with model",
			rc:   runConfig{model: "gpt-5.3-codex"},
			want: []string{"exec", "-s", "workspace-write", "--skip-git-repo-check", "-m", "gpt-5.3-codex", "-"},
		},
		{
			name: "whitespace-only model treated as empty",
			rc:   runConfig{model: "   "},
			want: []string{"exec", "-s", "workspace-write", "--skip-git-repo-check", "-"},
		},
		{
			name: "sandbox read-only",
			rc:   runConfig{sandbox: "read-only"},
			want: []string{"exec", "-s", "read-only", "--skip-git-repo-check", "-"},
		},
		{
			name: "sandbox danger-full-access uses bypass flag, not -s",
			rc:   runConfig{sandbox: "danger-full-access"},
			want: []string{"exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "-"},
		},
		{
			name: "all knobs together",
			rc:   runConfig{model: "gpt-5.3-codex", sandbox: "read-only", outputFormat: "json"},
			want: []string{"exec", "--json", "-s", "read-only", "--skip-git-repo-check", "-m", "gpt-5.3-codex", "-"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildCodexArgs(tc.rc, tc.streaming)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("buildCodexArgs(%+v, streaming=%v)\n  got:  %#v\n  want: %#v", tc.rc, tc.streaming, got, tc.want)
			}
		})
	}
}

func TestCodex_Name(t *testing.T) {
	t.Parallel()
	if got := NewCodex(Config{}).Name(); got != "codex" {
		t.Fatalf("Name() = %q, want %q", got, "codex")
	}
}

func TestNewAgent_Codex(t *testing.T) {
	t.Parallel()
	a, err := NewAgent(Config{Type: "codex"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if _, ok := a.(*Codex); !ok {
		t.Fatalf("NewAgent returned %T, want *Codex", a)
	}
	if a.Name() != "codex" {
		t.Fatalf("Name() = %q, want %q", a.Name(), "codex")
	}
}

func TestWithSandbox(t *testing.T) {
	t.Parallel()

	cfg := Config{Sandbox: "workspace-write"}

	rc := resolveRunConfig(cfg, nil)
	if rc.sandbox != "workspace-write" {
		t.Fatalf("sandbox = %q, want config default %q", rc.sandbox, "workspace-write")
	}

	rc = resolveRunConfig(cfg, []RunOption{WithSandbox("danger-full-access")})
	if rc.sandbox != "danger-full-access" {
		t.Fatalf("sandbox = %q, want override %q", rc.sandbox, "danger-full-access")
	}
}

// TestCodex_Run_FakeBinary points codexBinary at a temp shell script that
// echoes its args and stdin, proving the stdin-piping + argv wiring without a
// real Codex login.
func TestCodex_Run_FakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary uses a POSIX shell script")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex")
	const body = `#!/bin/sh
echo "args: $@"
echo "stdin:"
cat
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	defer func(orig string) { codexBinary = orig }(codexBinary)
	codexBinary = script

	const prompt = "implement the feature, please"
	c := NewCodex(Config{})
	res, err := c.Run(context.Background(), prompt, dir)
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (out=%q)", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, prompt) {
		t.Fatalf("Output = %q, want to contain piped prompt %q", res.Output, prompt)
	}
	// The argv should include the `exec` subcommand and the `-` stdin marker.
	if !strings.Contains(res.Output, "exec") || !strings.Contains(res.Output, "-") {
		t.Fatalf("Output = %q, want to contain argv 'exec ... -'", res.Output)
	}
}
