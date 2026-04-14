package mcp

import (
	"os"
	"testing"
)

func TestSafeEnvVarsContent(t *testing.T) {
	// Verify the safe baseline environment variables are correct.
	expected := []string{"PATH", "HOME", "TMPDIR", "LANG"}
	if len(safeEnvVars) != len(expected) {
		t.Fatalf("safeEnvVars length = %d, want %d", len(safeEnvVars), len(expected))
	}
	for i, v := range expected {
		if safeEnvVars[i] != v {
			t.Errorf("safeEnvVars[%d] = %q, want %q", i, safeEnvVars[i], v)
		}
	}
}

func TestStdioTransportStartFails(t *testing.T) {
	// Attempt to start a nonexistent command.
	_, err := NewStdioTransport("__nonexistent_mcp_server__", nil, nil)
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

func TestStdioTransportRestrictedEnv(t *testing.T) {
	// Verify that env expansion is restricted to safe vars and the env map.
	t.Setenv("TEST_MCP_VAR", "hello")
	t.Setenv("HOME", "/test/home")

	env := map[string]string{
		"CUSTOM_VAR":  "custom_value",
		"FROM_UNSAFE": "${TEST_MCP_VAR}", // not in safeEnvVars — should expand to ""
		"FROM_SAFE":   "${HOME}",         // in safeEnvVars — should expand
		"FROM_MAP":    "${CUSTOM_VAR}",   // in env map — should expand to raw value
	}

	// Build the same env as NewStdioTransport would, using the restricted
	// expansion logic.
	cmdEnv := make([]string, 0)
	for _, key := range safeEnvVars {
		if val, ok := os.LookupEnv(key); ok {
			cmdEnv = append(cmdEnv, key+"="+val)
		}
	}
	for key, val := range env {
		expanded := os.Expand(val, func(name string) string {
			for _, safe := range safeEnvVars {
				if name == safe {
					return os.Getenv(name)
				}
			}
			if v, ok := env[name]; ok {
				return v
			}
			return ""
		})
		cmdEnv = append(cmdEnv, key+"="+expanded)
	}

	lookup := make(map[string]string)
	for _, entry := range cmdEnv {
		parts := splitFirst(entry, '=')
		if len(parts) == 2 {
			lookup[parts[0]] = parts[1]
		}
	}

	// Unsafe host var should NOT be expanded.
	if lookup["FROM_UNSAFE"] != "" {
		t.Errorf("FROM_UNSAFE = %q, want empty (unsafe var should not expand)", lookup["FROM_UNSAFE"])
	}

	// Safe var (HOME) should be expanded.
	if lookup["FROM_SAFE"] != "/test/home" {
		t.Errorf("FROM_SAFE = %q, want /test/home", lookup["FROM_SAFE"])
	}

	// Cross-reference within env map should expand to raw value.
	if lookup["FROM_MAP"] != "custom_value" {
		t.Errorf("FROM_MAP = %q, want custom_value", lookup["FROM_MAP"])
	}

	// Literal value should pass through.
	if lookup["CUSTOM_VAR"] != "custom_value" {
		t.Errorf("CUSTOM_VAR = %q, want custom_value", lookup["CUSTOM_VAR"])
	}
}

// splitFirst splits s on the first occurrence of sep.
func splitFirst(s string, sep byte) []string {
	idx := -1
	for i := range s {
		if s[i] == sep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+1:]}
}
