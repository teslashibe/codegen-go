// Package mcp exposes the codegen-go [codegen.Agent] surface as a set of
// MCP (Model Context Protocol) tools that any host application can mount on
// its own MCP server.
//
// codegen-go is a small library that drives coding-agent CLIs (Claude Code,
// OpenAI Codex, Aider, OpenHands, Cline, etc.) behind a single
// [codegen.Agent] interface. The MCP wrappers in this package surface that
// surface so an LLM agent can delegate sub-tasks to a coding-agent CLI: hand
// it a prompt and a working directory and let it edit files in place.
//
// Each tool is defined via [mcptool.Define] so the JSON input schema is
// reflected from the typed input struct — no hand-maintained schemas, no
// drift.
//
// The "client" passed to each tool is a [codegen.Agent] (typically
// [*codegen.ClaudeCode] or [*codegen.GenericCLI]). The host application is
// responsible for constructing it; see [codegen.NewAgent].
//
// Usage from a host application:
//
//	import (
//	    "github.com/teslashibe/mcptool"
//	    codegen "github.com/teslashibe/codegen-go"
//	    cgmcp "github.com/teslashibe/codegen-go/mcp"
//	)
//
//	agent, _ := codegen.NewAgent(codegen.Config{Type: "claude-code"})
//	for _, tool := range cgmcp.Provider{}.Tools() {
//	    // register tool with your MCP server, passing agent as the
//	    // opaque client argument when invoking
//	}
//
// # Safety
//
// codegen_run executes a CLI inside the host process with full filesystem
// access (it edits files in work_dir and may shell out further depending on
// the underlying CLI's permissions model). Treat it as the most powerful
// tool in your inventory: gate it behind explicit user consent, run untrusted
// prompts in a sandbox (container, VM, ephemeral worktree), and apply
// per-call timeouts and output caps.
//
// The [Excluded] map documents methods on [codegen.Agent] that are
// intentionally not exposed via MCP, with a one-line reason. The coverage
// test in mcp_test.go fails if a new exported method is added without either
// being wrapped by a tool or appearing in [Excluded].
package mcp

import "github.com/teslashibe/mcptool"

// Provider implements [mcptool.Provider] for codegen-go. The zero value is
// ready to use.
type Provider struct{}

// Platform returns "codegen".
func (Provider) Platform() string { return "codegen" }

// Tools returns every codegen-go MCP tool, in registration order.
func (Provider) Tools() []mcptool.Tool {
	out := make([]mcptool.Tool, 0, len(runTools))
	out = append(out, runTools...)
	return out
}
