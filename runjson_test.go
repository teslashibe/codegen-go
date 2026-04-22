package codegen

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubAgent struct {
	out string
	err error
}

func (s stubAgent) Name() string { return "stub" }
func (s stubAgent) Run(_ context.Context, _ string, _ string, _ ...RunOption) (Result, error) {
	return Result{Output: s.out}, s.err
}

func TestRunJSON_Clean(t *testing.T) {
	var got struct {
		Verdict string `json:"verdict"`
	}
	err := RunJSON(context.Background(), stubAgent{out: `{"verdict":"APPROVE"}`}, "p", "/tmp", &got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Verdict != "APPROVE" {
		t.Fatalf("verdict = %q, want APPROVE", got.Verdict)
	}
}

func TestRunJSON_Fenced(t *testing.T) {
	out := "```json\n{\"verdict\":\"REQUEST_CHANGES\",\"summary\":\"x\"}\n```"
	var got struct {
		Verdict string `json:"verdict"`
		Summary string `json:"summary"`
	}
	if err := RunJSON(context.Background(), stubAgent{out: out}, "p", "/tmp", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Verdict != "REQUEST_CHANGES" || got.Summary != "x" {
		t.Fatalf("got %+v", got)
	}
}

func TestRunJSON_FencedNoLanguage(t *testing.T) {
	out := "```\n[1,2,3]\n```"
	var got []int
	if err := RunJSON(context.Background(), stubAgent{out: out}, "p", "/tmp", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0] != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestRunJSON_ProseWrapper(t *testing.T) {
	out := "Here is my analysis: {\"met\": true, \"reason\": \"all good\"} — that's the verdict."
	var got struct {
		Met    bool   `json:"met"`
		Reason string `json:"reason"`
	}
	if err := RunJSON(context.Background(), stubAgent{out: out}, "p", "/tmp", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Met || got.Reason != "all good" {
		t.Fatalf("got %+v", got)
	}
}

func TestRunJSON_ProseWithBracesInString(t *testing.T) {
	// A `}` inside a string literal must NOT terminate the object.
	out := `prelude {"summary":"weird } string","ok":true} trailing`
	var got struct {
		Summary string `json:"summary"`
		OK      bool   `json:"ok"`
	}
	if err := RunJSON(context.Background(), stubAgent{out: out}, "p", "/tmp", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary != "weird } string" || !got.OK {
		t.Fatalf("got %+v", got)
	}
}

func TestRunJSON_Malformed(t *testing.T) {
	var got map[string]any
	err := RunJSON(context.Background(), stubAgent{out: "not even close"}, "p", "/tmp", &got)
	if err == nil {
		t.Fatal("expected error for output with no JSON")
	}
	if !strings.Contains(err.Error(), "no JSON") {
		t.Fatalf("expected no-JSON error, got: %v", err)
	}
}

func TestRunJSON_TruncatedJSON(t *testing.T) {
	var got map[string]any
	err := RunJSON(context.Background(), stubAgent{out: `{"a": 1, "b":`}, "p", "/tmp", &got)
	if err == nil {
		t.Fatal("expected error for unmatched braces")
	}
}

func TestRunJSON_InvalidJSONInsideBraces(t *testing.T) {
	var got map[string]any
	// braces match but content is not valid JSON
	err := RunJSON(context.Background(), stubAgent{out: `{not valid json at all}`}, "p", "/tmp", &got)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got: %v", err)
	}
}

func TestRunJSON_AgentError(t *testing.T) {
	var got map[string]any
	wantErr := errors.New("boom")
	err := RunJSON(context.Background(), stubAgent{err: wantErr}, "p", "/tmp", &got)
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped agent error, got: %v", err)
	}
}

func TestRunJSON_NilAgent(t *testing.T) {
	var got map[string]any
	if err := RunJSON(context.Background(), nil, "p", "/tmp", &got); err == nil {
		t.Fatal("expected error for nil agent")
	}
}
