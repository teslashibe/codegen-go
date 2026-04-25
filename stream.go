// Package codegen — Stream helper for structured agent output.
//
// Coding-agent CLIs that support `--output-format stream-json` emit one
// JSON object per line describing the running session: init metadata,
// assistant turns, tool calls / results, and a final result summary.
// Stream invokes such an agent and decodes those lines into typed
// [StreamEvent] values, invoking the supplied callback for each.
//
// This is the streaming counterpart to [RunJSON]. Where RunJSON waits for
// the process to exit and decodes a single JSON value from buffered
// stdout, Stream surfaces incremental progress so callers can drive a
// dashboard, ticker, or activity feed without waiting for the full turn.
package codegen

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// StreamEvent is one decoded line from a streaming-capable agent CLI.
//
// The shape mirrors Claude Code's `stream-json` schema closely. Common
// top-level discriminators (Type, Subtype, SessionID) are parsed into
// typed fields; everything else is preserved verbatim in Raw so callers
// can decode richer payloads (Anthropic Message blocks, tool I/O, usage
// counters, MCP server lists) without forcing this package to track an
// evolving wire format.
type StreamEvent struct {
	// Type is the top-level discriminator: typically "system",
	// "assistant", "user", or "result".
	Type string `json:"type"`
	// Subtype refines Type. For Type=="system" this is "init"; for
	// Type=="result" it is "success" or one of the error_* variants.
	Subtype string `json:"subtype,omitempty"`
	// SessionID identifies the running Claude Code session. Stable across
	// every event in a single Stream invocation.
	SessionID string `json:"session_id,omitempty"`

	// Message is the embedded Anthropic Message for assistant/user events.
	// Left as RawMessage so callers can json.Unmarshal into the SDK type
	// of their choice without us bringing the SDK in as a dependency.
	Message json.RawMessage `json:"message,omitempty"`
	// ParentToolUseID links sub-agent invocations back to the tool call
	// that spawned them. Empty for top-level events.
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`

	// Result-only fields (populated when Type == "result").
	Result        string  `json:"result,omitempty"`
	DurationMS    int64   `json:"duration_ms,omitempty"`
	DurationAPIMS int64   `json:"duration_api_ms,omitempty"`
	NumTurns      int     `json:"num_turns,omitempty"`
	IsError       bool    `json:"is_error,omitempty"`
	TotalCostUSD  float64 `json:"total_cost_usd,omitempty"`

	// Raw is the original NDJSON line so callers that need fields we did
	// not promote (cwd, mcp_servers, tools, permission_denials, usage,
	// custom future additions, ...) can decode them independently.
	Raw json.RawMessage `json:"-"`
}

// streamEventHandler is invoked for each parsed line. Returning a non-nil
// error aborts the scan and propagates back from Stream.
type streamEventHandler = func(StreamEvent)

// streamingAgent is implemented by agents that can emit `stream-json` on
// stdout. ClaudeCode is the only built-in implementation today; external
// agents may opt in by satisfying this interface.
//
// It is intentionally unexported: callers don't need to construct one,
// they just pass a normal Agent to Stream and we type-assert.
type streamingAgent interface {
	Agent
	streamCommand(rc runConfig) (binary string, args []string)
}

// streamCommand implements streamingAgent for ClaudeCode.
func (c *ClaudeCode) streamCommand(rc runConfig) (string, []string) {
	return claudeBinary, buildClaudeArgs(rc, true)
}

// ErrStreamUnsupported is returned by Stream when the supplied Agent does
// not implement streaming output (e.g. GenericCLI, third-party agents).
var ErrStreamUnsupported = errors.New("codegen: agent does not support streaming output")

// streamScannerBuf is the per-line capacity for the NDJSON scanner. Big
// assistant turns can serialize to a few hundred KB (long answers, large
// tool results); 8 MiB gives ample headroom without committing the memory
// upfront (bufio.Scanner grows on demand up to this cap).
const streamScannerBuf = 8 * 1024 * 1024

// Stream invokes agent in streaming mode and calls onEvent for each
// NDJSON line emitted on stdout. Streaming requires the agent to support
// `stream-json` output (currently only [ClaudeCode]); for any other
// agent Stream returns [ErrStreamUnsupported] before exec.
//
// The onEvent callback runs on the scanner goroutine. Slow callbacks
// throttle the producer process (which is usually the desired
// behaviour); if you need to fan out asynchronously, push events onto a
// buffered channel inside the callback.
//
// The returned Result captures process duration, exit code, and any
// stderr output (truncated to MaxOutputBytes). Stdout bytes are consumed
// by the parser and are not echoed into Result.Output.
//
// Streaming forces `--output-format stream-json --verbose` regardless of
// any OutputFormat setting; everything else (model, system prompt, MCP
// config, allowed tools) flows through unchanged.
func Stream(
	ctx context.Context,
	agent Agent,
	prompt, workDir string,
	onEvent func(StreamEvent),
	opts ...RunOption,
) (Result, error) {
	if agent == nil {
		return Result{ExitCode: -1}, fmt.Errorf("codegen.Stream: agent is nil")
	}
	if onEvent == nil {
		return Result{ExitCode: -1}, fmt.Errorf("codegen.Stream: onEvent is nil")
	}

	sa, ok := agent.(streamingAgent)
	if !ok {
		return Result{ExitCode: -1}, fmt.Errorf("%w: %s", ErrStreamUnsupported, agent.Name())
	}

	rc := resolveRunConfig(extractAgentConfig(agent), opts)
	binary, args := sa.streamCommand(rc)

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(prompt)
	if env := buildChildEnv(rc.unsetEnv); env != nil {
		cmd.Env = env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("codegen.Stream: stdout pipe: %w", err)
	}
	stderrBuf := &cappedBuffer{max: rc.maxOutputBytes}
	cmd.Stderr = stderrBuf

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Output: stderrBuf.String()}, fmt.Errorf("%s: start: %w", binary, err)
	}

	scanErr := scanStream(stdout, onEvent)
	waitErr := cmd.Wait()
	duration := time.Since(start)

	res := Result{
		Output:    stderrBuf.String(),
		Duration:  duration,
		Truncated: stderrBuf.truncated,
	}
	switch {
	case waitErr == nil:
		res.ExitCode = 0
	case cmd.ProcessState != nil:
		res.ExitCode = cmd.ProcessState.ExitCode()
	default:
		res.ExitCode = -1
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return res, fmt.Errorf("%s: exit code %d: %w", binary, res.ExitCode, waitErr)
		}
		return res, fmt.Errorf("%s: %w", binary, waitErr)
	}
	if scanErr != nil {
		return res, fmt.Errorf("%s: scan stdout: %w", binary, scanErr)
	}
	return res, nil
}

// scanStream reads NDJSON lines from r, decodes each into a StreamEvent,
// and dispatches it to onEvent. Lines that fail to parse are skipped
// silently (they're typically progress chatter or warnings, not the
// event stream proper); the caller can inspect Result.Output (stderr)
// for diagnostics if events go missing.
func scanStream(r io.Reader, onEvent func(StreamEvent)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), streamScannerBuf)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// stream-json events are always JSON objects; a leading non-{
		// byte means it's stray text we should drop.
		if line[0] != '{' {
			continue
		}
		var ev StreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		ev.Raw = append(json.RawMessage(nil), line...)
		onEvent(ev)
	}
	return scanner.Err()
}

// extractAgentConfig pulls the embedded Config off built-in agent types so
// Stream() can re-resolve options the same way Run() does. External agents
// that satisfy streamingAgent without exposing their Config still work —
// the empty Config simply means RunOptions take full responsibility for
// non-default values.
func extractAgentConfig(a Agent) Config {
	switch v := a.(type) {
	case *ClaudeCode:
		return v.cfg
	case *GenericCLI:
		return v.cfg
	default:
		return Config{}
	}
}
