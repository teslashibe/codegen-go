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

func TestBuildOpenCodeArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rc        runConfig
		streaming bool
		want      []string
	}{
		{
			name: "defaults: just the run subcommand",
			rc:   runConfig{},
			want: []string{"run"},
		},
		{
			name: "explicit json output format adds --format json",
			rc:   runConfig{outputFormat: "json"},
			want: []string{"run", "--format", "json"},
		},
		{
			name:      "streaming forces --format json",
			rc:        runConfig{},
			streaming: true,
			want:      []string{"run", "--format", "json"},
		},
		{
			name:      "streaming overrides non-json outputFormat",
			rc:        runConfig{outputFormat: "text"},
			streaming: true,
			want:      []string{"run", "--format", "json"},
		},
		{
			name: "skip permissions",
			rc:   runConfig{skipPermissions: true},
			want: []string{"run", "--dangerously-skip-permissions"},
		},
		{
			name: "with provider/model model passed verbatim",
			rc:   runConfig{model: "anthropic/claude-sonnet-4-5"},
			want: []string{"run", "-m", "anthropic/claude-sonnet-4-5"},
		},
		{
			name: "whitespace-only model treated as empty",
			rc:   runConfig{model: "   "},
			want: []string{"run"},
		},
		{
			name: "with variant",
			rc:   runConfig{variant: "high"},
			want: []string{"run", "--variant", "high"},
		},
		{
			name: "whitespace-only variant treated as empty",
			rc:   runConfig{variant: "  "},
			want: []string{"run"},
		},
		{
			name: "all knobs together",
			rc: runConfig{
				model:           "anthropic/claude-sonnet-4-5",
				variant:         "max",
				outputFormat:    "json",
				skipPermissions: true,
			},
			want: []string{
				"run", "--format", "json", "--dangerously-skip-permissions",
				"-m", "anthropic/claude-sonnet-4-5", "--variant", "max",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildOpenCodeArgs(tc.rc, tc.streaming)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("buildOpenCodeArgs(%+v, streaming=%v)\n  got:  %#v\n  want: %#v", tc.rc, tc.streaming, got, tc.want)
			}
		})
	}
}

func TestNewAgent_OpenCode(t *testing.T) {
	t.Parallel()
	a, err := NewAgent(Config{Type: "opencode"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if _, ok := a.(*OpenCode); !ok {
		t.Fatalf("NewAgent returned %T, want *OpenCode", a)
	}
	if a.Name() != "opencode" {
		t.Fatalf("Name() = %q, want %q", a.Name(), "opencode")
	}
}

func TestOpenCode_Name(t *testing.T) {
	t.Parallel()
	if got := NewOpenCode(Config{}).Name(); got != "opencode" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestOpenCode_Run_FakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake binary")
	}

	// A fake `opencode` that echoes its args and the piped stdin so we can
	// assert both the argv wiring and that the prompt arrives on stdin.
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	body := "#!/bin/sh\necho \"args: $@\"\necho \"stdin:\"\ncat\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	orig := opencodeBinary
	opencodeBinary = script
	defer func() { opencodeBinary = orig }()

	const prompt = "implement the feature\nwith two lines"
	res, err := NewOpenCode(Config{}).Run(context.Background(), prompt, dir,
		WithSkipPermissions(true))
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (out=%q)", res.ExitCode, res.Output)
	}
	// Prompt was piped on stdin (verified mechanism for `opencode run`).
	if !strings.Contains(res.Output, prompt) {
		t.Fatalf("Output = %q, want to contain piped prompt %q", res.Output, prompt)
	}
	// argv wiring: run subcommand and the skip-permissions flag.
	for _, want := range []string{"run", "--dangerously-skip-permissions"} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("Output = %q, want to contain argv fragment %q", res.Output, want)
		}
	}
}

func TestWithVariant(t *testing.T) {
	t.Parallel()

	rc := resolveRunConfig(Config{Variant: "minimal"}, []RunOption{
		WithVariant("high"),
	})
	if rc.variant != "high" {
		t.Fatalf("variant = %q, want %q (RunOption should override Config)", rc.variant, "high")
	}

	// Config value flows through when no option overrides it.
	rc = resolveRunConfig(Config{Variant: "max"}, nil)
	if rc.variant != "max" {
		t.Fatalf("variant = %q, want %q (Config default)", rc.variant, "max")
	}
}

func TestWithSkipPermissions(t *testing.T) {
	t.Parallel()

	// RunOption overrides Config (true over false default).
	rc := resolveRunConfig(Config{}, []RunOption{
		WithSkipPermissions(true),
	})
	if !rc.skipPermissions {
		t.Fatalf("skipPermissions = %v, want true (RunOption should override Config)", rc.skipPermissions)
	}

	// RunOption can also turn it back off.
	rc = resolveRunConfig(Config{SkipPermissions: true}, []RunOption{
		WithSkipPermissions(false),
	})
	if rc.skipPermissions {
		t.Fatalf("skipPermissions = %v, want false (RunOption should override Config)", rc.skipPermissions)
	}

	// Config value flows through when no option overrides it.
	rc = resolveRunConfig(Config{SkipPermissions: true}, nil)
	if !rc.skipPermissions {
		t.Fatalf("skipPermissions = %v, want true (Config default)", rc.skipPermissions)
	}
}

func TestOpenCode_Stream_FakeBinary(t *testing.T) {
	// No t.Parallel(): mutates the package-level opencodeBinary var.
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake binary")
	}

	// A fake `opencode` emitting two NDJSON-ish lines and exiting 0. OpenCode's
	// schema differs from Claude's; we only rely on Raw being preserved.
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"step_start\",\"sessionID\":\"ses_1\"}'\n" +
		"printf '%s\\n' '{\"type\":\"text\",\"part\":{\"text\":\"OK\"}}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	orig := opencodeBinary
	opencodeBinary = script
	defer func() { opencodeBinary = orig }()

	var got []StreamEvent
	res, err := Stream(context.Background(), NewOpenCode(Config{}), "do it", dir, func(ev StreamEvent) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("Stream: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(got) != 2 {
		t.Fatalf("event count = %d, want 2: %#v", len(got), got)
	}
	if got[0].Type != "step_start" || got[1].Type != "text" {
		t.Errorf("unexpected events: %#v", got)
	}
	// Raw preserves the original line so callers can decode OpenCode's schema.
	if !strings.Contains(string(got[1].Raw), `"text":"OK"`) {
		t.Errorf("Raw missing original field: %s", got[1].Raw)
	}
}

// TestOpenCode_Run_RealCLI is an opt-in smoke test against a real `opencode`
// install. Skipped when the binary is absent.
func TestOpenCode_Run_RealCLI(t *testing.T) {
	skipIfMissing(t, "opencode")

	res, err := NewOpenCode(Config{}).Run(
		context.Background(),
		"reply with the single word OK",
		t.TempDir(),
		WithSkipPermissions(true),
	)
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (out=%q)", res.ExitCode, res.Output)
	}
}
