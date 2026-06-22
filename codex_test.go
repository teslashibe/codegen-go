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
			name: "explicit json output format adds --json before sandbox",
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
			name: "sandbox danger-full-access bypasses approvals and omits -s",
			rc:   runConfig{sandbox: "danger-full-access"},
			want: []string{"exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "-"},
		},
		{
			name: "all knobs together",
			rc: runConfig{
				model:        "gpt-5.3-codex",
				sandbox:      "read-only",
				outputFormat: "json",
			},
			streaming: false,
			want: []string{
				"exec", "--json", "-s", "read-only", "--skip-git-repo-check",
				"-m", "gpt-5.3-codex", "-",
			},
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

func TestCodex_Name(t *testing.T) {
	t.Parallel()
	if got := NewCodex(Config{}).Name(); got != "codex" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestCodex_Run_FakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake binary")
	}

	// A fake `codex` that echoes its args and the piped stdin so we can assert
	// both the argv wiring and that the prompt arrives on stdin.
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\necho \"args: $@\"\necho \"stdin:\"\ncat\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	orig := codexBinary
	codexBinary = script
	defer func() { codexBinary = orig }()

	const prompt = "implement the feature\nwith two lines"
	res, err := NewCodex(Config{}).Run(context.Background(), prompt, dir)
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (out=%q)", res.ExitCode, res.Output)
	}
	// Prompt was piped on stdin.
	if !strings.Contains(res.Output, prompt) {
		t.Fatalf("Output = %q, want to contain piped prompt %q", res.Output, prompt)
	}
	// argv wiring: exec subcommand, default sandbox, stdin marker.
	for _, want := range []string{"exec", "-s workspace-write", "--skip-git-repo-check", " -"} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("Output = %q, want to contain argv fragment %q", res.Output, want)
		}
	}
}

func TestWithSandbox(t *testing.T) {
	t.Parallel()

	rc := resolveRunConfig(Config{Sandbox: "workspace-write"}, []RunOption{
		WithSandbox("danger-full-access"),
	})
	if rc.sandbox != "danger-full-access" {
		t.Fatalf("sandbox = %q, want %q (RunOption should override Config)", rc.sandbox, "danger-full-access")
	}

	// Config value flows through when no option overrides it.
	rc = resolveRunConfig(Config{Sandbox: "read-only"}, nil)
	if rc.sandbox != "read-only" {
		t.Fatalf("sandbox = %q, want %q (Config default)", rc.sandbox, "read-only")
	}
}
