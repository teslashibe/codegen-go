package codegen

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestBuildOpenCodeArgs(t *testing.T) {
	t.Parallel()

	trueValue := true
	falseValue := false
	tests := []struct {
		name      string
		rc        runConfig
		streaming bool
		want      []string
	}{
		{"defaults", runConfig{}, false, []string{"run"}},
		{"json output", runConfig{outputFormat: "json"}, false, []string{"run", "--format", "json"}},
		{"streaming", runConfig{}, true, []string{"run", "--format", "json"}},
		{"model", runConfig{model: "anthropic/claude-sonnet-4"}, false, []string{"run", "-m", "anthropic/claude-sonnet-4"}},
		{"variant", runConfig{variant: "high"}, false, []string{"run", "--variant", "high"}},
		{"skip permissions", runConfig{skipPermissions: &trueValue}, false, []string{"run", "--dangerously-skip-permissions"}},
		{"no skip permissions", runConfig{skipPermissions: &falseValue}, false, []string{"run"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildOpenCodeArgs(tc.rc, tc.streaming); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestOpenCode_Run_FakeBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary is unix-only")
	}
	old := opencodeBinary
	t.Cleanup(func() { opencodeBinary = old })
	opencodeBinary = writeFakeScript(t, "opencode", "printf 'args:%s\\n' \"$*\"; printf 'stdin:'; cat")

	res, err := NewOpenCode(Config{}).Run(context.Background(), "hello opencode", t.TempDir(), WithSkipPermissions(true), WithVariant("max"))
	if err != nil {
		t.Fatalf("Run: %v (out=%q)", err, res.Output)
	}
	if !strings.Contains(res.Output, "args:run --variant max --dangerously-skip-permissions") {
		t.Fatalf("Output = %q, missing args", res.Output)
	}
	if !strings.Contains(res.Output, "stdin:hello opencode") {
		t.Fatalf("Output = %q, missing stdin prompt", res.Output)
	}
}

func TestWithSkipPermissionsAndVariant(t *testing.T) {
	t.Parallel()
	rc := resolveRunConfig(Config{Variant: "high"}, []RunOption{WithSkipPermissions(true), WithVariant("max")})
	if rc.skipPermissions == nil || !*rc.skipPermissions {
		t.Fatalf("skipPermissions = %#v, want true", rc.skipPermissions)
	}
	if rc.variant != "max" {
		t.Fatalf("variant = %q, want max", rc.variant)
	}
}
