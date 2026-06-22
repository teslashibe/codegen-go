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

func TestBuildGeminiArgs(t *testing.T) {
	t.Parallel()

	const prompt = "reply with the single word OK"

	tests := []struct {
		name      string
		rc        runConfig
		streaming bool
		prompt    string
		want      []string
	}{
		{
			name:   "defaults: text output, yolo approval",
			rc:     runConfig{},
			prompt: prompt,
			want:   []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-p", prompt},
		},
		{
			name:   "explicit json output format",
			rc:     runConfig{outputFormat: "json"},
			prompt: prompt,
			want:   []string{"-o", "json", "--approval-mode", "yolo", "--skip-trust", "-p", prompt},
		},
		{
			name:      "streaming forces stream-json",
			rc:        runConfig{},
			streaming: true,
			prompt:    prompt,
			want:      []string{"-o", "stream-json", "--approval-mode", "yolo", "--skip-trust", "-p", prompt},
		},
		{
			name:      "streaming overrides outputFormat",
			rc:        runConfig{outputFormat: "json"},
			streaming: true,
			prompt:    prompt,
			want:      []string{"-o", "stream-json", "--approval-mode", "yolo", "--skip-trust", "-p", prompt},
		},
		{
			name:   "with model",
			rc:     runConfig{model: "gemini-2.5-pro"},
			prompt: prompt,
			want:   []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-m", "gemini-2.5-pro", "-p", prompt},
		},
		{
			name:   "whitespace-only model treated as empty",
			rc:     runConfig{model: "   "},
			prompt: prompt,
			want:   []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-p", prompt},
		},
		{
			name:   "approval mode plan is read-only",
			rc:     runConfig{approvalMode: "plan"},
			prompt: prompt,
			want:   []string{"-o", "text", "--approval-mode", "plan", "--skip-trust", "-p", prompt},
		},
		{
			name:   "whitespace-only approval mode falls back to yolo",
			rc:     runConfig{approvalMode: "  "},
			prompt: prompt,
			want:   []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-p", prompt},
		},
		{
			name: "all knobs together",
			rc: runConfig{
				model:        "gemini-2.5-pro",
				approvalMode: "auto_edit",
				outputFormat: "json",
			},
			prompt: prompt,
			want: []string{
				"-o", "json", "--approval-mode", "auto_edit", "--skip-trust",
				"-m", "gemini-2.5-pro", "-p", prompt,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildGeminiArgs(tc.rc, tc.streaming, tc.prompt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("buildGeminiArgs(%+v, streaming=%v)\n  got:  %#v\n  want: %#v", tc.rc, tc.streaming, got, tc.want)
			}
		})
	}
}

func TestNewAgent_Gemini(t *testing.T) {
	t.Parallel()
	a, err := NewAgent(Config{Type: "gemini"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if _, ok := a.(*Gemini); !ok {
		t.Fatalf("NewAgent returned %T, want *Gemini", a)
	}
	if a.Name() != "gemini" {
		t.Fatalf("Name() = %q, want %q", a.Name(), "gemini")
	}
}

func TestGemini_Name(t *testing.T) {
	t.Parallel()
	if got := NewGemini(Config{}).Name(); got != "gemini" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestGemini_Run_FakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake binary")
	}

	// A fake `gemini` that echoes its args so we can assert the argv wiring.
	// Approach A passes the prompt in argv (-p) and nothing on stdin.
	dir := t.TempDir()
	script := filepath.Join(dir, "gemini")
	body := "#!/bin/sh\necho \"args: $@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	orig := geminiBinary
	geminiBinary = script
	defer func() { geminiBinary = orig }()

	const prompt = "implement the feature"
	res, err := NewGemini(Config{}).Run(context.Background(), prompt, dir,
		WithApprovalMode("plan"))
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (out=%q)", res.ExitCode, res.Output)
	}
	// Prompt arrives via -p argv (Approach A), not stdin.
	if !strings.Contains(res.Output, prompt) {
		t.Fatalf("Output = %q, want to contain prompt %q in argv", res.Output, prompt)
	}
	// argv wiring: headless -p, approval mode, trust skip.
	for _, want := range []string{"-p", "--approval-mode plan", "--skip-trust", "-o text"} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("Output = %q, want to contain argv fragment %q", res.Output, want)
		}
	}
}

func TestWithApprovalMode(t *testing.T) {
	t.Parallel()

	rc := resolveRunConfig(Config{ApprovalMode: "yolo"}, []RunOption{
		WithApprovalMode("plan"),
	})
	if rc.approvalMode != "plan" {
		t.Fatalf("approvalMode = %q, want %q (RunOption should override Config)", rc.approvalMode, "plan")
	}

	// Config value flows through when no option overrides it.
	rc = resolveRunConfig(Config{ApprovalMode: "auto_edit"}, nil)
	if rc.approvalMode != "auto_edit" {
		t.Fatalf("approvalMode = %q, want %q (Config default)", rc.approvalMode, "auto_edit")
	}
}

func TestGemini_Stream_FakeBinary(t *testing.T) {
	// No t.Parallel(): mutates the package-level geminiBinary var.
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake binary")
	}

	// A fake `gemini` emitting two stream-json-ish lines and exiting 0. Gemini's
	// schema differs from Claude's; we only rely on Raw being preserved.
	dir := t.TempDir()
	script := filepath.Join(dir, "gemini")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"text\":\"hello\"}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"ok\":true}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	orig := geminiBinary
	geminiBinary = script
	defer func() { geminiBinary = orig }()

	var got []StreamEvent
	res, err := Stream(context.Background(), NewGemini(Config{}), "do it", dir, func(ev StreamEvent) {
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
	if got[0].Type != "assistant" || got[1].Type != "result" {
		t.Errorf("unexpected events: %#v", got)
	}
	// Raw preserves the original line so callers can decode Gemini's schema.
	if !strings.Contains(string(got[1].Raw), `"ok":true`) {
		t.Errorf("Raw missing original field: %s", got[1].Raw)
	}
}

// TestGemini_Run_RealCLI is an opt-in smoke test against a real `gemini`
// install. Skipped when the binary is absent.
func TestGemini_Run_RealCLI(t *testing.T) {
	skipIfMissing(t, "gemini")

	res, err := NewGemini(Config{}).Run(
		context.Background(),
		"reply with the single word OK",
		t.TempDir(),
		WithApprovalMode("plan"),
	)
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (out=%q)", res.ExitCode, res.Output)
	}
}
