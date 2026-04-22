// Package codegen — RunJSON helper.
//
// Coding-agent CLIs (Claude Code, Codex, etc.) emit free-form text by
// default. When we use the agent as a structured reasoner — verifier,
// reviewer, triager — we need a JSON value out of that text. RunJSON
// runs the agent and decodes the first well-formed JSON object or array
// from its output, tolerating markdown code fences and surrounding
// prose.
package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RunJSON executes agent with prompt and decodes the agent's stdout as
// JSON into out. The agent is expected to produce a single JSON object
// or array; RunJSON tolerates output wrapped in markdown fences
// (```json ... ```) or surrounded by prose, extracting the first
// well-formed JSON value via a brace-counting parse (not regex).
//
// Returns a clear error if no JSON value is found, or if the extracted
// substring fails to unmarshal into out.
func RunJSON(ctx context.Context, agent Agent, prompt, workDir string, out any, opts ...RunOption) error {
	if agent == nil {
		return fmt.Errorf("codegen.RunJSON: agent is nil")
	}
	if out == nil {
		return fmt.Errorf("codegen.RunJSON: out is nil")
	}

	res, err := agent.Run(ctx, prompt, workDir, opts...)
	if err != nil {
		return fmt.Errorf("codegen.RunJSON: agent %s run failed: %w", agent.Name(), err)
	}

	cleaned := stripMarkdownFences(res.Output)
	jsonStr, ok := extractFirstJSON(cleaned)
	if !ok {
		return fmt.Errorf("codegen.RunJSON: no JSON object or array found in agent output (len=%d)", len(res.Output))
	}
	if err := json.Unmarshal([]byte(jsonStr), out); err != nil {
		return fmt.Errorf("codegen.RunJSON: unmarshal extracted json: %w (json=%q)", err, truncate(jsonStr, 512))
	}
	return nil
}

// stripMarkdownFences trims a leading ```json / ``` opener and the
// matching trailing ``` so we can hand the inner payload to the JSON
// extractor. We only strip one fence pair — anything else is treated as
// prose around a JSON value and handled by extractFirstJSON.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	rest := s[3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		// Drop the language hint line (```json, ```\n, etc.).
		rest = rest[nl+1:]
	} else {
		rest = strings.TrimPrefix(rest, "json")
	}
	if idx := strings.LastIndex(rest, "```"); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.TrimSpace(rest)
}

// extractFirstJSON finds the first `{...}` or `[...]` value in s using
// a brace-counting parser that respects strings and escapes. Returns
// the substring and true on success.
func extractFirstJSON(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			if end, ok := matchClosing(s, i, '{', '}'); ok {
				return s[i : end+1], true
			}
		case '[':
			if end, ok := matchClosing(s, i, '[', ']'); ok {
				return s[i : end+1], true
			}
		}
	}
	return "", false
}

// matchClosing scans from start (which must point at open) and returns
// the index of the matching close, accounting for nested pairs and
// string literals (with escape sequences). Returns false if no match.
func matchClosing(s string, start int, open, close byte) (int, bool) {
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escape {
				escape = false
				continue
			}
			switch c {
			case '\\':
				escape = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
