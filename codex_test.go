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
		{"defaults", runConfig{}, false, []string{"exec", "-s", "workspace-write", "--skip-git-repo-check", "-"}},
		{"json output", runConfig{outputFormat: "json"}, false, []string{"exec", "--json", "-s", "workspace-write", "--skip-git-repo-check", "-"}},
		{"streaming", runConfig{}, true, []string{"exec", "--json", "-s", "workspace-write", "--skip-git-repo-check", "-"}},
		{"model", runConfig{model: "gpt-5.3-codex"}, false, []string{"exec", "-s", "workspace-write", "--skip-git-repo-check", "-m", "gpt-5.3-codex", "-"}},
		{"blank model", runConfig{model: "   "}, false, []string{"exec", "-s", "workspace-write", "--skip-git-repo-check", "-"}},
		{"read-only sandbox", runConfig{sandbox: "read-only"}, false, []string{"exec", "-s", "read-only", "--skip-git-repo-check", "-"}},
		{"danger full access", runConfig{sandbox: "danger-full-access"}, false, []string{"exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "-"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildCodexArgs(tc.rc, tc.streaming); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestCodex_Run_FakeBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary is unix-only")
	}
	old := codexBinary
	t.Cleanup(func() { codexBinary = old })
	codexBinary = writeFakeScript(t, "codex", "printf 'args:%s\\n' \"$*\"; printf 'stdin:'; cat")

	res, err := NewCodex(Config{}).Run(context.Background(), "hello codex", t.TempDir(), WithSandbox("read-only"))
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if !strings.Contains(res.Output, "args:exec -s read-only --skip-git-repo-check -") {
		t.Fatalf("Output = %q, missing args", res.Output)
	}
	if !strings.Contains(res.Output, "stdin:hello codex") {
		t.Fatalf("Output = %q, missing stdin prompt", res.Output)
	}
}

func TestWithSandbox(t *testing.T) {
	t.Parallel()
	rc := resolveRunConfig(Config{Sandbox: "workspace-write"}, []RunOption{WithSandbox("read-only")})
	if rc.sandbox != "read-only" {
		t.Fatalf("sandbox = %q, want read-only", rc.sandbox)
	}
}

func writeFakeScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake script: %v", err)
	}
	return path
}
