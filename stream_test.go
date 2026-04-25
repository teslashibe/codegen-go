package codegen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeClaude builds a tiny shell script that mimics Claude Code's
// stream-json output: emits the supplied lines on stdout (one per line),
// optionally prints diagnostics on stderr, and exits with the given code.
//
// Lines may contain embedded newlines escaped as \\n; the script un-escapes
// them so a single "line" can be a multi-line JSON object if needed (the
// scanner splits on real newlines so callers should not rely on that for
// unit tests). Returns the absolute path to the script.
func fakeClaude(t *testing.T, stdoutLines []string, stderrText string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fakeClaude uses /bin/sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, line := range stdoutLines {
		// printf %s avoids interpreting backslashes in the JSON.
		b.WriteString(fmt.Sprintf("printf '%%s\\n' %s\n", shellQuote(line)))
	}
	if stderrText != "" {
		b.WriteString(fmt.Sprintf("printf '%%s' %s 1>&2\n", shellQuote(stderrText)))
	}
	b.WriteString(fmt.Sprintf("exit %d\n", exitCode))

	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}

// shellQuote wraps s in single quotes for /bin/sh, escaping any embedded
// single quotes via the standard '\'' dance.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// withClaudeBinary swaps the package-level claudeBinary for the duration of
// the test and restores it on cleanup. This mirrors the pattern used by
// the existing GenericCLI tests for `echo`/`cat`.
func withClaudeBinary(t *testing.T, path string) {
	t.Helper()
	prev := claudeBinary
	claudeBinary = path
	t.Cleanup(func() { claudeBinary = prev })
}

func TestStream_RejectsNonStreamingAgent(t *testing.T) {
	t.Parallel()
	g := NewGenericCLI(Config{Command: "echo"})
	_, err := Stream(context.Background(), g, "p", t.TempDir(), func(StreamEvent) {})
	if !errors.Is(err, ErrStreamUnsupported) {
		t.Fatalf("err = %v, want ErrStreamUnsupported", err)
	}
}

func TestStream_RejectsNilArgs(t *testing.T) {
	t.Parallel()
	c := NewClaudeCode(Config{})

	if _, err := Stream(context.Background(), nil, "p", t.TempDir(), func(StreamEvent) {}); err == nil {
		t.Errorf("nil agent: want error")
	}
	if _, err := Stream(context.Background(), c, "p", t.TempDir(), nil); err == nil {
		t.Errorf("nil onEvent: want error")
	}
}

func TestStream_DecodesEventsInOrder(t *testing.T) {
	// No t.Parallel(): withClaudeBinary mutates the package-level
	// claudeBinary var, so binary-installing tests must run serially.
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-4","cwd":"/work"}`,
		`{"type":"assistant","session_id":"sess-1","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"user","session_id":"sess-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}`,
		`{"type":"result","subtype":"success","session_id":"sess-1","is_error":false,"duration_ms":1234,"num_turns":2,"result":"done","total_cost_usd":0.0123}`,
	}
	bin := fakeClaude(t, lines, "", 0)
	withClaudeBinary(t, bin)

	c := NewClaudeCode(Config{})
	var got []StreamEvent
	res, err := Stream(context.Background(), c, "ignored prompt", t.TempDir(), func(ev StreamEvent) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("Stream: %v (out=%q)", err, res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(got) != 4 {
		t.Fatalf("event count = %d, want 4: %#v", len(got), got)
	}

	if got[0].Type != "system" || got[0].Subtype != "init" || got[0].SessionID != "sess-1" {
		t.Errorf("init event = %+v", got[0])
	}
	// Raw should be the original line, allowing callers to extract the
	// "model" / "cwd" fields we did not promote.
	if !strings.Contains(string(got[0].Raw), `"model":"claude-opus-4"`) {
		t.Errorf("Raw missing model: %s", got[0].Raw)
	}

	if got[1].Type != "assistant" || len(got[1].Message) == 0 {
		t.Errorf("assistant event = %+v", got[1])
	}
	// Assistant Message should round-trip through json.Unmarshal.
	var msg struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(got[1].Message, &msg); err != nil {
		t.Fatalf("unmarshal assistant message: %v", err)
	}
	if msg.Role != "assistant" || len(msg.Content) != 1 || msg.Content[0].Text != "hello" {
		t.Errorf("assistant message = %+v", msg)
	}

	if got[3].Type != "result" || got[3].Subtype != "success" || got[3].IsError {
		t.Errorf("result event = %+v", got[3])
	}
	if got[3].DurationMS != 1234 || got[3].NumTurns != 2 || got[3].Result != "done" {
		t.Errorf("result fields = %+v", got[3])
	}
	if got[3].TotalCostUSD != 0.0123 {
		t.Errorf("TotalCostUSD = %v", got[3].TotalCostUSD)
	}
}

func TestStream_SkipsBlankAndNonJSONLines(t *testing.T) {
	lines := []string{
		"",
		"warning: deprecated flag",
		`{"type":"system","subtype":"init","session_id":"x"}`,
		"trailing prose",
		`{"type":"result","subtype":"success","session_id":"x","is_error":false}`,
	}
	bin := fakeClaude(t, lines, "", 0)
	withClaudeBinary(t, bin)

	var got []StreamEvent
	_, err := Stream(context.Background(), NewClaudeCode(Config{}), "p", t.TempDir(), func(ev StreamEvent) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("event count = %d, want 2 (system + result), got %#v", len(got), got)
	}
	if got[0].Type != "system" || got[1].Type != "result" {
		t.Errorf("event order wrong: %#v", got)
	}
}

func TestStream_PropagatesNonZeroExit(t *testing.T) {
	lines := []string{`{"type":"result","subtype":"error_max_turns","session_id":"x","is_error":true}`}
	bin := fakeClaude(t, lines, "boom on stderr\n", 2)
	withClaudeBinary(t, bin)

	var events []StreamEvent
	res, err := Stream(context.Background(), NewClaudeCode(Config{}), "p", t.TempDir(), func(ev StreamEvent) {
		events = append(events, ev)
	})
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", res.ExitCode)
	}
	if !strings.Contains(res.Output, "boom on stderr") {
		t.Errorf("stderr not captured: %q", res.Output)
	}
	// Even on non-zero exit we should have parsed the events emitted before
	// the process died.
	if len(events) != 1 || !events[0].IsError {
		t.Errorf("events = %#v", events)
	}
}

func TestStream_ContextCancelStopsProcess(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skipf("sleep not on PATH: %v", err)
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	withClaudeBinary(t, bin)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Stream(ctx, NewClaudeCode(Config{}), "p", dir, func(StreamEvent) {})
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on cancelled context")
		}
	case <-contextDeadlineHelper():
		t.Fatal("Stream did not return after cancel within 3s")
	}
}

// contextDeadlineHelper is a tiny helper that yields a channel firing in
// 3 seconds; using a variable avoids importing time in the test body just
// for one timeout, keeping test diagnostics tidy.
func contextDeadlineHelper() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		// 3 seconds is plenty for an exec.Cmd kill on cancel.
		_, _ = exec.Command("sleep", "3").CombinedOutput()
		close(ch)
	}()
	return ch
}
