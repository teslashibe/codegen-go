package mcp

import (
	"context"
	"fmt"
	"time"

	codegen "github.com/teslashibe/codegen-go"
	"github.com/teslashibe/mcptool"
)

// RunInput is the typed input shared by codegen_run and codegen_run_json.
type RunInput struct {
	Prompt         string `json:"prompt" jsonschema:"description=task prompt sent to the coding-agent CLI on stdin,required"`
	WorkDir        string `json:"work_dir" jsonschema:"description=absolute working directory the agent runs in (and may modify in place),required"`
	Model          string `json:"model,omitempty" jsonschema:"description=optional model override (passed to claude --model; ignored by GenericCLI)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"description=per-call timeout in seconds; 0 keeps the agent's Config default (30m); negative disables,minimum=-1"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty" jsonschema:"description=cap on captured combined stdout/stderr in bytes; 0 keeps the agent's Config default (10 MiB); negative disables,minimum=-1"`
}

// RunOutput is the response shape for codegen_run. It mirrors
// [codegen.Result] but uses JSON-friendly types (duration as milliseconds
// rather than time.Duration's nanosecond integer).
type RunOutput struct {
	AgentName  string `json:"agent_name"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// RunJSONOutput is the response shape for codegen_run_json. The decoded JSON
// value is surfaced as Value; AgentName and DurationMs document which agent
// produced the value and how long it took.
type RunJSONOutput struct {
	AgentName  string `json:"agent_name"`
	DurationMs int64  `json:"duration_ms"`
	Value      any    `json:"value"`
}

// buildRunOptions converts the JSON-friendly RunInput overrides into
// [codegen.RunOption] values. Zero values fall through to the agent's
// Config defaults; negative values disable the corresponding cap.
func buildRunOptions(in RunInput) []codegen.RunOption {
	var opts []codegen.RunOption
	if in.Model != "" {
		opts = append(opts, codegen.WithModel(in.Model))
	}
	if in.TimeoutSeconds != 0 {
		opts = append(opts, codegen.WithTimeout(time.Duration(in.TimeoutSeconds)*time.Second))
	}
	if in.MaxOutputBytes != 0 {
		opts = append(opts, codegen.WithMaxOutputBytes(in.MaxOutputBytes))
	}
	return opts
}

func runAgent(ctx context.Context, a codegen.Agent, in RunInput) (any, error) {
	if a == nil {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "codegen agent is nil"}
	}
	if in.Prompt == "" {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "prompt must not be empty"}
	}
	if in.WorkDir == "" {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "work_dir must not be empty"}
	}

	res, err := a.Run(ctx, in.Prompt, in.WorkDir, buildRunOptions(in)...)
	out := RunOutput{
		AgentName:  a.Name(),
		Output:     res.Output,
		ExitCode:   res.ExitCode,
		DurationMs: res.Duration.Milliseconds(),
		Truncated:  res.Truncated,
	}
	if err != nil {
		return nil, &mcptool.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("%s: %v", out.AgentName, err),
			Data: map[string]any{
				"agent_name":       out.AgentName,
				"exit_code":        out.ExitCode,
				"duration_ms":      out.DurationMs,
				"truncated_output": out.Truncated,
				"output_tail":      tailString(out.Output, 4000),
			},
		}
	}
	return out, nil
}

func runAgentJSON(ctx context.Context, a codegen.Agent, in RunInput) (any, error) {
	if a == nil {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "codegen agent is nil"}
	}
	if in.Prompt == "" {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "prompt must not be empty"}
	}
	if in.WorkDir == "" {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "work_dir must not be empty"}
	}

	start := time.Now()
	var value any
	err := codegen.RunJSON(ctx, a, in.Prompt, in.WorkDir, &value, buildRunOptions(in)...)
	dur := time.Since(start)
	if err != nil {
		return nil, &mcptool.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("%s: %v", a.Name(), err),
			Data: map[string]any{
				"agent_name":  a.Name(),
				"duration_ms": dur.Milliseconds(),
			},
		}
	}
	return RunJSONOutput{
		AgentName:  a.Name(),
		DurationMs: dur.Milliseconds(),
		Value:      value,
	}, nil
}

// tailString returns the last max bytes of s, prefixed with an ellipsis when
// truncation occurred. Used to keep failure responses bounded in size while
// still giving the agent the most-recent output (where errors typically
// appear).
func tailString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}

var runTools = []mcptool.Tool{
	mcptool.Define[codegen.Agent, RunInput](
		"codegen_run",
		"Execute a coding-agent CLI (Claude Code or generic) with a prompt against a working directory; returns captured output",
		"Run",
		runAgent,
	),
	mcptool.Define[codegen.Agent, RunInput](
		"codegen_run_json",
		"Run a coding-agent CLI as a structured reasoner; decodes the first JSON object/array in its output",
		"",
		runAgentJSON,
	),
}
