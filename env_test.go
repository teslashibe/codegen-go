package codegen

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWithUnsetEnv_AccumulatesAndClears(t *testing.T) {
	t.Parallel()

	rc := resolveRunConfig(Config{}, []RunOption{
		WithUnsetEnv("FOO"),
		WithUnsetEnv("BAR", "BAZ"),
	})
	if !reflect.DeepEqual(rc.unsetEnv, []string{"FOO", "BAR", "BAZ"}) {
		t.Fatalf("unsetEnv = %v, want [FOO BAR BAZ]", rc.unsetEnv)
	}

	rc = resolveRunConfig(Config{}, []RunOption{
		WithUnsetEnv("FOO"),
		WithUnsetEnv(),
	})
	if rc.unsetEnv != nil {
		t.Fatalf("unsetEnv = %v, want nil after empty WithUnsetEnv()", rc.unsetEnv)
	}
}

func TestBuildChildEnv_NilWhenEmpty(t *testing.T) {
	t.Parallel()
	if env := buildChildEnv(nil); env != nil {
		t.Fatalf("buildChildEnv(nil) = %v, want nil", env)
	}
	if env := buildChildEnv([]string{}); env != nil {
		t.Fatalf("buildChildEnv([]) = %v, want nil", env)
	}
}

func TestBuildChildEnv_FiltersNamedKeys(t *testing.T) {
	t.Setenv("CODEGEN_TEST_KEEP", "1")
	t.Setenv("CODEGEN_TEST_DROP", "secret")
	t.Setenv("CODEGEN_TEST_ALSO_DROP", "secret2")

	env := buildChildEnv([]string{"CODEGEN_TEST_DROP", "CODEGEN_TEST_ALSO_DROP", "NOT_PRESENT"})
	if slices.ContainsFunc(env, hasKey("CODEGEN_TEST_DROP")) {
		t.Errorf("CODEGEN_TEST_DROP leaked into child env")
	}
	if slices.ContainsFunc(env, hasKey("CODEGEN_TEST_ALSO_DROP")) {
		t.Errorf("CODEGEN_TEST_ALSO_DROP leaked into child env")
	}
	if !slices.ContainsFunc(env, hasKey("CODEGEN_TEST_KEEP")) {
		t.Errorf("CODEGEN_TEST_KEEP missing from filtered env")
	}
}

// TestStream_StripsNamedEnv proves the surface area we ship to
// callers: a fake claude script echoing $ANTHROPIC_API_KEY in its
// stdout-emitted JSON should see an empty value when the option is
// supplied. Without the option the child sees the parent's value.
// This is the headline behaviour callers (polybot/localagent) depend
// on to force claude-code's subscription auth.
func TestStream_StripsNamedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake claude uses /bin/sh")
	}
	t.Setenv("FAKE_ANTHROPIC_KEY", "leak-this")

	dir := t.TempDir()
	path := dir + "/claude"
	script := `#!/bin/sh
printf '{"type":"system","subtype":"init","value":"%s"}\n' "${FAKE_ANTHROPIC_KEY:-}"
printf '{"type":"result","subtype":"success","result":"ok"}\n'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	withClaudeBinary(t, path)

	for _, tc := range []struct {
		name     string
		opts     []RunOption
		wantLeak bool
	}{
		{"default inherits parent env", nil, true},
		{"WithUnsetEnv strips the key", []RunOption{WithUnsetEnv("FAKE_ANTHROPIC_KEY")}, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var captured string
			c := NewClaudeCode(Config{})
			_, err := Stream(context.Background(), c, "p", t.TempDir(), func(ev StreamEvent) {
				if ev.Type == "system" && strings.Contains(string(ev.Raw), "value") {
					captured = string(ev.Raw)
				}
			}, tc.opts...)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			leaked := strings.Contains(captured, `"value":"leak-this"`)
			if leaked != tc.wantLeak {
				t.Fatalf("leak=%v want=%v (raw=%s)", leaked, tc.wantLeak, captured)
			}
		})
	}
}

func hasKey(key string) func(string) bool {
	return func(kv string) bool {
		eq := strings.IndexByte(kv, '=')
		return eq >= 0 && envKeyEqual(kv[:eq], key)
	}
}
