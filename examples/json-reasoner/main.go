// json-reasoner: ask Claude Code for a structured verdict and decode it
// straight into a Go struct via codegen.RunJSON.
//
// Usage:
//
//	go run ./examples/json-reasoner /path/to/repo
//
// The agent is asked to score the cleanliness of the working tree and return
// a JSON object — useful as a template for verifier / reviewer roles.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	codegen "github.com/teslashibe/codegen-go"
)

type Verdict struct {
	Score  int      `json:"score"`  // 0..100
	Reason string   `json:"reason"` // one sentence
	Risks  []string `json:"risks"`
}

const prompt = `Inspect the current directory's git working tree and rate it
0–100 on cleanliness (commits, branch hygiene, untracked files). Respond with a
SINGLE JSON object exactly matching this schema and NOTHING ELSE:

{"score": int, "reason": "string", "risks": ["string", ...]}`

func main() {
	workDir, _ := os.Getwd()
	if len(os.Args) >= 2 {
		workDir = os.Args[1]
	}

	agent := codegen.NewClaudeCode(codegen.Config{})

	var v Verdict
	if err := codegen.RunJSON(context.Background(), agent, prompt, workDir, &v); err != nil {
		log.Fatalf("RunJSON: %v", err)
	}

	pretty, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(pretty))
}
