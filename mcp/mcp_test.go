package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	codegen "github.com/teslashibe/codegen-go"
	cgmcp "github.com/teslashibe/codegen-go/mcp"
	"github.com/teslashibe/mcptool"
)

// TestEveryClientMethodIsWrappedOrExcluded fails when a new exported method
// is added to [codegen.Agent] without either being wrapped by an MCP tool or
// being added to cgmcp.Excluded with a reason. This is the drift-prevention
// mechanism: keeping the MCP surface in lockstep with the package API is
// enforced by CI rather than convention.
func TestEveryClientMethodIsWrappedOrExcluded(t *testing.T) {
	rep := mcptool.Coverage(
		reflect.TypeOf((*codegen.Agent)(nil)).Elem(),
		cgmcp.Provider{}.Tools(),
		cgmcp.Excluded,
	)
	if len(rep.Missing) > 0 {
		t.Fatalf("methods missing MCP exposure (add a tool or list in excluded.go): %v", rep.Missing)
	}
	if len(rep.UnknownExclusions) > 0 {
		t.Fatalf("excluded.go references methods that don't exist on codegen.Agent (rename?): %v", rep.UnknownExclusions)
	}
	if len(rep.Wrapped)+len(rep.Excluded) == 0 {
		t.Fatal("no wrapped or excluded methods detected — coverage helper is mis-configured")
	}
}

// TestToolsValidate verifies every tool has a non-empty name in canonical
// snake_case form, a description within length limits, and a non-nil Invoke
// + InputSchema.
func TestToolsValidate(t *testing.T) {
	if err := mcptool.ValidateTools(cgmcp.Provider{}.Tools()); err != nil {
		t.Fatal(err)
	}
}

// TestPlatformName guards against accidental rebrands.
func TestPlatformName(t *testing.T) {
	if got := (cgmcp.Provider{}).Platform(); got != "codegen" {
		t.Errorf("Platform() = %q, want codegen", got)
	}
}

// TestToolsHaveCodegenPrefix encodes the per-platform naming convention.
func TestToolsHaveCodegenPrefix(t *testing.T) {
	const prefix = "codegen_"
	for _, tool := range (cgmcp.Provider{}).Tools() {
		if !strings.HasPrefix(tool.Name, prefix) {
			t.Errorf("tool %q lacks %s prefix", tool.Name, prefix)
		}
	}
}

// TestToolNamesAreUnique sanity-checks that we haven't accidentally
// registered a duplicate tool (ValidateTools also enforces this, but an
// explicit test pinpoints the failure).
func TestToolNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range (cgmcp.Provider{}).Tools() {
		if seen[tool.Name] {
			t.Fatalf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
	}
}

// stubAgent is a fake codegen.Agent driven by a function. Used to exercise
// the tool handlers without spawning real CLI processes.
type stubAgent struct {
	name string
	run  func(ctx context.Context, prompt, workDir string, opts ...codegen.RunOption) (codegen.Result, error)
}

func (s stubAgent) Name() string { return s.name }
func (s stubAgent) Run(ctx context.Context, prompt, workDir string, opts ...codegen.RunOption) (codegen.Result, error) {
	return s.run(ctx, prompt, workDir, opts...)
}

// findTool returns the first tool with the given name from the Provider's
// inventory, or fails the test.
func findTool(t *testing.T, name string) mcptool.Tool {
	t.Helper()
	for _, tool := range (cgmcp.Provider{}).Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered", name)
	return mcptool.Tool{}
}

func TestCodegenRun_Success(t *testing.T) {
	tool := findTool(t, "codegen_run")
	agent := stubAgent{
		name: "stub",
		run: func(_ context.Context, prompt, workDir string, _ ...codegen.RunOption) (codegen.Result, error) {
			if prompt != "do the thing" {
				t.Errorf("prompt = %q, want %q", prompt, "do the thing")
			}
			if workDir != "/tmp/work" {
				t.Errorf("workDir = %q, want %q", workDir, "/tmp/work")
			}
			return codegen.Result{Output: "all good", ExitCode: 0, Duration: 250 * time.Millisecond}, nil
		},
	}
	raw := json.RawMessage(`{"prompt":"do the thing","work_dir":"/tmp/work"}`)
	got, err := tool.Invoke(context.Background(), codegen.Agent(agent), raw)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out, ok := got.(cgmcp.RunOutput)
	if !ok {
		t.Fatalf("Invoke returned %T, want RunOutput", got)
	}
	if out.AgentName != "stub" {
		t.Errorf("AgentName = %q", out.AgentName)
	}
	if out.Output != "all good" {
		t.Errorf("Output = %q", out.Output)
	}
	if out.ExitCode != 0 {
		t.Errorf("ExitCode = %d", out.ExitCode)
	}
	if out.DurationMs != 250 {
		t.Errorf("DurationMs = %d, want 250", out.DurationMs)
	}
}

func TestCodegenRun_AgentFailureIsStructured(t *testing.T) {
	tool := findTool(t, "codegen_run")
	agent := stubAgent{
		name: "stub",
		run: func(_ context.Context, _, _ string, _ ...codegen.RunOption) (codegen.Result, error) {
			return codegen.Result{Output: "boom — exit 2", ExitCode: 2, Duration: time.Second}, errors.New("stub: exit code 2")
		},
	}
	raw := json.RawMessage(`{"prompt":"go","work_dir":"/x"}`)
	_, err := tool.Invoke(context.Background(), codegen.Agent(agent), raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *mcptool.Error, got %T (%v)", err, err)
	}
	if toolErr.Code != "agent_failed" {
		t.Errorf("Code = %q, want agent_failed", toolErr.Code)
	}
	if toolErr.Data["exit_code"] != 2 {
		t.Errorf("Data[exit_code] = %v, want 2", toolErr.Data["exit_code"])
	}
	if got, _ := toolErr.Data["output_tail"].(string); !strings.Contains(got, "boom") {
		t.Errorf("Data[output_tail] = %q, want substring 'boom'", got)
	}
}

func TestCodegenRun_RejectsEmptyPrompt(t *testing.T) {
	tool := findTool(t, "codegen_run")
	agent := stubAgent{name: "stub", run: func(context.Context, string, string, ...codegen.RunOption) (codegen.Result, error) {
		t.Fatal("agent.Run must not be called when prompt is empty")
		return codegen.Result{}, nil
	}}
	_, err := tool.Invoke(context.Background(), codegen.Agent(agent), json.RawMessage(`{"work_dir":"/tmp"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" {
		t.Fatalf("expected invalid_input, got %v", err)
	}
}

func TestCodegenRun_RejectsEmptyWorkDir(t *testing.T) {
	tool := findTool(t, "codegen_run")
	agent := stubAgent{name: "stub", run: func(context.Context, string, string, ...codegen.RunOption) (codegen.Result, error) {
		t.Fatal("agent.Run must not be called when work_dir is empty")
		return codegen.Result{}, nil
	}}
	_, err := tool.Invoke(context.Background(), codegen.Agent(agent), json.RawMessage(`{"prompt":"x"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var toolErr *mcptool.Error
	if !errors.As(err, &toolErr) || toolErr.Code != "invalid_input" {
		t.Fatalf("expected invalid_input, got %v", err)
	}
}

func TestCodegenRunJSON_DecodesObject(t *testing.T) {
	tool := findTool(t, "codegen_run_json")
	agent := stubAgent{
		name: "stub",
		run: func(_ context.Context, _, _ string, _ ...codegen.RunOption) (codegen.Result, error) {
			return codegen.Result{Output: "prefix prose ```json\n{\"approve\":true,\"reason\":\"ok\"}\n```", ExitCode: 0, Duration: 5 * time.Millisecond}, nil
		},
	}
	raw := json.RawMessage(`{"prompt":"verify","work_dir":"/x"}`)
	got, err := tool.Invoke(context.Background(), codegen.Agent(agent), raw)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out, ok := got.(cgmcp.RunJSONOutput)
	if !ok {
		t.Fatalf("Invoke returned %T, want RunJSONOutput", got)
	}
	m, ok := out.Value.(map[string]any)
	if !ok {
		t.Fatalf("Value = %T (%v), want map[string]any", out.Value, out.Value)
	}
	if m["approve"] != true {
		t.Errorf("approve = %v, want true", m["approve"])
	}
	if m["reason"] != "ok" {
		t.Errorf("reason = %v, want ok", m["reason"])
	}
}

func TestCodegenRun_OptionsAreApplied(t *testing.T) {
	tool := findTool(t, "codegen_run")
	var sawOpts int
	agent := stubAgent{
		name: "stub",
		run: func(_ context.Context, _, _ string, opts ...codegen.RunOption) (codegen.Result, error) {
			sawOpts = len(opts)
			return codegen.Result{Output: "ok"}, nil
		},
	}
	raw := json.RawMessage(`{"prompt":"x","work_dir":"/y","model":"opus","timeout_seconds":30,"max_output_bytes":1024}`)
	if _, err := tool.Invoke(context.Background(), codegen.Agent(agent), raw); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if sawOpts != 3 {
		t.Errorf("opts forwarded = %d, want 3", sawOpts)
	}
}

func TestCodegenRun_WrongClientType(t *testing.T) {
	tool := findTool(t, "codegen_run")
	_, err := tool.Invoke(context.Background(), "not-an-agent", json.RawMessage(`{"prompt":"x","work_dir":"/y"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, mcptool.ErrWrongClientType) {
		t.Errorf("err = %v, want ErrWrongClientType", err)
	}
}
