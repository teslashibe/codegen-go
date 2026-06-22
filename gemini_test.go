package codegen

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestBuildGeminiArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rc        runConfig
		streaming bool
		want      []string
	}{
		{"defaults", runConfig{}, false, []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-p", "prompt"}},
		{"json output", runConfig{outputFormat: "json"}, false, []string{"-o", "json", "--approval-mode", "yolo", "--skip-trust", "-p", "prompt"}},
		{"streaming", runConfig{}, true, []string{"-o", "stream-json", "--approval-mode", "yolo", "--skip-trust", "-p", "prompt"}},
		{"model", runConfig{model: "gemini-3-pro"}, false, []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-m", "gemini-3-pro", "-p", "prompt"}},
		{"approval plan", runConfig{approvalMode: "plan"}, false, []string{"-o", "text", "--approval-mode", "plan", "--skip-trust", "-p", "prompt"}},
		{"blank model", runConfig{model: "   "}, false, []string{"-o", "text", "--approval-mode", "yolo", "--skip-trust", "-p", "prompt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildGeminiArgs(tc.rc, tc.streaming, "prompt"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestGemini_Run_FakeBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary is unix-only")
	}
	old := geminiBinary
	t.Cleanup(func() { geminiBinary = old })
	geminiBinary = writeFakeScript(t, "gemini", "printf 'args:%s\\n' \"$*\"; printf 'stdin:'; cat")

	res, err := NewGemini(Config{}).Run(context.Background(), "hello gemini", t.TempDir(), WithApprovalMode("plan"))
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if !strings.Contains(res.Output, "args:-o text --approval-mode plan --skip-trust -p hello gemini") {
		t.Fatalf("Output = %q, missing args/prompt", res.Output)
	}
}

func TestWithApprovalMode(t *testing.T) {
	t.Parallel()
	rc := resolveRunConfig(Config{ApprovalMode: "yolo"}, []RunOption{WithApprovalMode("plan")})
	if rc.approvalMode != "plan" {
		t.Fatalf("approvalMode = %q, want plan", rc.approvalMode)
	}
}
