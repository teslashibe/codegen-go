package codegen

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipIfMissing(t *testing.T, bin string) {
	t.Helper()
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("%s not on PATH: %v", bin, err)
	}
}

func TestNewAgent_Factory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		wantTyp string
	}{
		{"default empty", Config{}, false, "claude-code"},
		{"explicit claude", Config{Type: "claude-code"}, false, "claude-code"},
		{"generic", Config{Type: "generic", Command: "echo"}, false, "generic"},
		{"unknown", Config{Type: "bogus"}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewAgent(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got agent %#v", a)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Name() != tc.wantTyp {
				t.Fatalf("Name() = %q, want %q", a.Name(), tc.wantTyp)
			}
		})
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
		want  []string
	}{
		{
			name:  "no model",
			model: "",
			want:  []string{"-p", "--output-format", "text", "--dangerously-skip-permissions"},
		},
		{
			name:  "whitespace model treated as empty",
			model: "   ",
			want:  []string{"-p", "--output-format", "text", "--dangerously-skip-permissions"},
		},
		{
			name:  "with model",
			model: "opus",
			want:  []string{"-p", "--output-format", "text", "--dangerously-skip-permissions", "--model", "opus"},
		},
		{
			name:  "trims model whitespace",
			model: "  sonnet  ",
			want:  []string{"-p", "--output-format", "text", "--dangerously-skip-permissions", "--model", "sonnet"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildClaudeArgs(tc.model)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("buildClaudeArgs(%q)\n  got:  %#v\n  want: %#v", tc.model, got, tc.want)
			}
		})
	}
}

func TestClaudeCode_Name(t *testing.T) {
	t.Parallel()
	if got := NewClaudeCode(Config{}).Name(); got != "claude-code" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestGenericCLI_NoCommand(t *testing.T) {
	t.Parallel()
	g := NewGenericCLI(Config{})
	res, err := g.Run(context.Background(), "hi", t.TempDir())
	if err == nil {
		t.Fatalf("expected error, got %#v", res)
	}
	if res.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1", res.ExitCode)
	}
}

func TestGenericCLI_OutputCapture(t *testing.T) {
	t.Parallel()
	skipIfMissing(t, "echo")

	g := NewGenericCLI(Config{Command: "echo", Args: []string{"hello", "world"}})
	res, err := g.Run(context.Background(), "", t.TempDir())
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if !strings.Contains(res.Output, "hello world") {
		t.Fatalf("Output = %q, want to contain %q", res.Output, "hello world")
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Truncated {
		t.Fatalf("Truncated = true, want false")
	}
	if res.Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", res.Duration)
	}
}

func TestGenericCLI_StdinPromptDelivery(t *testing.T) {
	t.Parallel()
	skipIfMissing(t, "cat")

	const prompt = "this is the prompt body\nwith two lines"
	g := NewGenericCLI(Config{Command: "cat"})
	res, err := g.Run(context.Background(), prompt, t.TempDir())
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if res.Output != prompt {
		t.Fatalf("Output = %q, want %q", res.Output, prompt)
	}
}

func TestGenericCLI_ContextTimeout(t *testing.T) {
	t.Parallel()
	skipIfMissing(t, "sleep")

	g := NewGenericCLI(Config{Command: "sleep", Args: []string{"5"}})
	start := time.Now()
	res, err := g.Run(context.Background(), "", t.TempDir(), WithTimeout(50*time.Millisecond))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error, got nil (out=%q)", res.Output)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %v, expected fast cancellation", elapsed)
	}
	// Either a context error or an exec exit error caused by the kill is fine.
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "killed") && !strings.Contains(err.Error(), "signal") && !strings.Contains(err.Error(), "exit code") {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func TestGenericCLI_ParentContextCancel(t *testing.T) {
	t.Parallel()
	skipIfMissing(t, "sleep")

	ctx, cancel := context.WithCancel(context.Background())
	g := NewGenericCLI(Config{Command: "sleep", Args: []string{"5"}})

	done := make(chan error, 1)
	go func() {
		_, err := g.Run(ctx, "", t.TempDir(), WithTimeout(10*time.Second))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s after cancel")
	}
}

func TestGenericCLI_OutputTruncation(t *testing.T) {
	t.Parallel()
	skipIfMissing(t, "printf")

	// Print 1000 'a's. Cap output at 10 bytes.
	g := NewGenericCLI(Config{
		Command: "printf",
		Args:    []string{strings.Repeat("a", 1000)},
	})
	res, err := g.Run(context.Background(), "", t.TempDir(), WithMaxOutputBytes(10))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Output) != 10 {
		t.Fatalf("len(Output) = %d, want 10 (output=%q)", len(res.Output), res.Output)
	}
	if !res.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
	if res.Output != strings.Repeat("a", 10) {
		t.Fatalf("Output = %q, want %q", res.Output, strings.Repeat("a", 10))
	}
}

func TestGenericCLI_NonZeroExit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX false")
	}
	skipIfMissing(t, "false")

	g := NewGenericCLI(Config{Command: "false"})
	res, err := g.Run(context.Background(), "", t.TempDir())
	if err == nil {
		t.Fatalf("expected error from `false`, got nil")
	}
	if res.ExitCode == 0 {
		t.Fatalf("ExitCode = 0, want non-zero")
	}
}

func TestResolveRunConfig_Defaults(t *testing.T) {
	t.Parallel()

	rc := resolveRunConfig(Config{}, nil)
	if rc.timeout != DefaultTimeout {
		t.Fatalf("timeout = %v, want %v", rc.timeout, DefaultTimeout)
	}
	if rc.maxOutputBytes != DefaultMaxOutputBytes {
		t.Fatalf("maxOutputBytes = %d, want %d", rc.maxOutputBytes, DefaultMaxOutputBytes)
	}

	rc = resolveRunConfig(
		Config{Timeout: time.Second, MaxOutputBytes: 5, Model: "from-config"},
		[]RunOption{WithModel("override"), WithMaxOutputBytes(99)},
	)
	if rc.model != "override" {
		t.Fatalf("model = %q, want %q", rc.model, "override")
	}
	if rc.timeout != time.Second {
		t.Fatalf("timeout = %v, want 1s", rc.timeout)
	}
	if rc.maxOutputBytes != 99 {
		t.Fatalf("maxOutputBytes = %d, want 99", rc.maxOutputBytes)
	}
}

func TestCappedBuffer_Uncapped(t *testing.T) {
	t.Parallel()
	b := &cappedBuffer{max: 0}
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := b.Write([]byte(" world")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if b.String() != "hello world" {
		t.Fatalf("String() = %q", b.String())
	}
	if b.truncated {
		t.Fatal("truncated = true, want false")
	}
}
