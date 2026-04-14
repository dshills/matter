package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agent.MaxSteps != 20 {
		t.Errorf("MaxSteps = %d, want 20", cfg.Agent.MaxSteps)
	}
	if cfg.Agent.MaxDuration != 2*time.Minute {
		t.Errorf("MaxDuration = %v, want 2m", cfg.Agent.MaxDuration)
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", cfg.LLM.Model)
	}
	if cfg.Memory.RecentMessages != 10 {
		t.Errorf("RecentMessages = %d, want 10", cfg.Memory.RecentMessages)
	}
	if cfg.Memory.SummarizeAfterMessages != 15 {
		t.Errorf("SummarizeAfterMessages = %d, want 15", cfg.Memory.SummarizeAfterMessages)
	}
	if cfg.Tools.EnableWorkspaceRead != true {
		t.Error("EnableWorkspaceRead should default to true")
	}
	if cfg.Tools.EnableCommandExec != false {
		t.Error("EnableCommandExec should default to false")
	}
	if cfg.Sandbox.MaxOutputBytes != 1048576 {
		t.Errorf("MaxOutputBytes = %d, want 1048576", cfg.Sandbox.MaxOutputBytes)
	}
	if cfg.Observe.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.Observe.LogLevel)
	}
}

func TestLoadFromFile(t *testing.T) {
	yamlContent := `
agent:
  max_steps: 50
  max_cost_usd: 10.0
llm:
  provider: anthropic
  model: claude-sonnet-4-20250514
memory:
  recent_messages: 5
  summarize_after_messages: 20
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Overridden values
	if cfg.Agent.MaxSteps != 50 {
		t.Errorf("MaxSteps = %d, want 50", cfg.Agent.MaxSteps)
	}
	if cfg.Agent.MaxCostUSD != 10.0 {
		t.Errorf("MaxCostUSD = %f, want 10.0", cfg.Agent.MaxCostUSD)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want claude-sonnet-4-20250514", cfg.LLM.Model)
	}
	if cfg.Memory.RecentMessages != 5 {
		t.Errorf("RecentMessages = %d, want 5", cfg.Memory.RecentMessages)
	}

	// Defaults preserved for unset fields
	if cfg.Agent.MaxDuration != 2*time.Minute {
		t.Errorf("MaxDuration should keep default 2m, got %v", cfg.Agent.MaxDuration)
	}
	if cfg.Tools.EnableWorkspaceRead != true {
		t.Error("EnableWorkspaceRead should keep default true")
	}
	if cfg.Observe.LogLevel != "info" {
		t.Errorf("LogLevel should keep default info, got %q", cfg.Observe.LogLevel)
	}
}

func TestLoadFromFileMissing(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFromFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFromFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestApplyEnv(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("MATTER_AGENT_MAX_STEPS", "100")
	t.Setenv("MATTER_LLM_MODEL", "gpt-4o-mini")
	t.Setenv("MATTER_TOOLS_ENABLE_COMMAND_EXEC", "true")
	t.Setenv("MATTER_OBSERVE_LOG_LEVEL", "debug")
	t.Setenv("MATTER_AGENT_MAX_COST_USD", "5.50")
	t.Setenv("MATTER_SANDBOX_NETWORK_ENABLED", "1")

	cfg, err := ApplyEnv(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.MaxSteps != 100 {
		t.Errorf("MaxSteps = %d, want 100", cfg.Agent.MaxSteps)
	}
	if cfg.LLM.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q, want gpt-4o-mini", cfg.LLM.Model)
	}
	if !cfg.Tools.EnableCommandExec {
		t.Error("EnableCommandExec should be true after env override")
	}
	if cfg.Observe.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.Observe.LogLevel)
	}
	if cfg.Agent.MaxCostUSD != 5.50 {
		t.Errorf("MaxCostUSD = %f, want 5.50", cfg.Agent.MaxCostUSD)
	}
	if !cfg.Sandbox.NetworkEnabled {
		t.Error("NetworkEnabled should be true after env override")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	yamlContent := `
agent:
  max_steps: 50
llm:
  model: gpt-4o
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("MATTER_AGENT_MAX_STEPS", "200")
	cfg, err = ApplyEnv(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.MaxSteps != 200 {
		t.Errorf("env should override file: MaxSteps = %d, want 200", cfg.Agent.MaxSteps)
	}
	if cfg.LLM.Model != "gpt-4o" {
		t.Errorf("file value should be preserved when no env set: Model = %q", cfg.LLM.Model)
	}
}

func TestValidateOK(t *testing.T) {
	cfg := DefaultConfig()
	if err := Validate(cfg); err != nil {
		t.Errorf("default config should be valid: %v", err)
	}
}

func TestValidateSummarizeAfterMessages(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory.SummarizeAfterMessages = 10
	cfg.Memory.RecentMessages = 10
	err := Validate(cfg)
	if err == nil {
		t.Error("expected error when summarize_after_messages == recent_messages")
	}

	cfg.Memory.SummarizeAfterMessages = 5
	err = Validate(cfg)
	if err == nil {
		t.Error("expected error when summarize_after_messages < recent_messages")
	}
}

func TestValidateMaxSteps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.MaxSteps = 0
	err := Validate(cfg)
	if err == nil {
		t.Error("expected error for max_steps = 0")
	}
}

func TestValidateEmptyProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.Provider = ""
	if err := Validate(cfg); err == nil {
		t.Error("expected error for empty provider")
	}
}

func TestValidateEmptyModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.Model = ""
	if err := Validate(cfg); err == nil {
		t.Error("expected error for empty model")
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Setenv("MATTER_TEST_BOOL", tt.input)
		if got := envBool("MATTER_TEST_BOOL", false); got != tt.want {
			t.Errorf("envBool(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestEnvBoolFallback(t *testing.T) {
	if got := envBool("MATTER_UNSET_BOOL", true); !got {
		t.Error("envBool should return fallback when env is unset")
	}
}

func TestApplyEnvMalformedValue(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("MATTER_AGENT_MAX_STEPS", "notanumber")
	_, err := ApplyEnv(cfg)
	if err == nil {
		t.Error("expected error for malformed MATTER_AGENT_MAX_STEPS")
	}
}

func TestApplyEnvMalformedDuration(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("MATTER_AGENT_MAX_DURATION", "bad")
	_, err := ApplyEnv(cfg)
	if err == nil {
		t.Error("expected error for malformed MATTER_AGENT_MAX_DURATION")
	}
}

func TestApplyEnvMalformedFloat(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("MATTER_AGENT_MAX_COST_USD", "abc")
	_, err := ApplyEnv(cfg)
	if err == nil {
		t.Error("expected error for malformed MATTER_AGENT_MAX_COST_USD")
	}
}

func TestApplyEnvLLMNewFields(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("MATTER_LLM_API_KEY", "test-key")
	t.Setenv("MATTER_LLM_BASE_URL", "https://proxy.example.com")
	t.Setenv("MATTER_LLM_PRICING_FILE", "/tmp/pricing.json")
	t.Setenv("MATTER_LLM_FALLBACK_COST_PER_1K", "0.005")

	cfg, err := ApplyEnv(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.LLM.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want test-key", cfg.LLM.APIKey)
	}
	if cfg.LLM.BaseURL != "https://proxy.example.com" {
		t.Errorf("BaseURL = %q, want https://proxy.example.com", cfg.LLM.BaseURL)
	}
	if cfg.LLM.PricingFile != "/tmp/pricing.json" {
		t.Errorf("PricingFile = %q, want /tmp/pricing.json", cfg.LLM.PricingFile)
	}
	if cfg.LLM.FallbackCostPer1K != 0.005 {
		t.Errorf("FallbackCostPer1K = %f, want 0.005", cfg.LLM.FallbackCostPer1K)
	}
}

func TestApplyEnvMalformedFallbackCost(t *testing.T) {
	cfg := DefaultConfig()
	t.Setenv("MATTER_LLM_FALLBACK_COST_PER_1K", "notafloat")
	_, err := ApplyEnv(cfg)
	if err == nil {
		t.Error("expected error for malformed MATTER_LLM_FALLBACK_COST_PER_1K")
	}
}

func TestRedactConfigWithKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.APIKey = "sk-secret-key-12345"

	redacted := RedactConfig(cfg)
	if redacted.LLM.APIKey != "***" {
		t.Errorf("redacted APIKey = %q, want ***", redacted.LLM.APIKey)
	}
	// Original should not be modified.
	if cfg.LLM.APIKey != "sk-secret-key-12345" {
		t.Errorf("original APIKey modified: %q", cfg.LLM.APIKey)
	}
}

func TestRedactConfigEmptyKey(t *testing.T) {
	cfg := DefaultConfig()
	redacted := RedactConfig(cfg)
	if redacted.LLM.APIKey != "" {
		t.Errorf("empty APIKey should stay empty after redaction, got %q", redacted.LLM.APIKey)
	}
}

func TestRedactConfigExtraHeaders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.ExtraHeaders = map[string]string{
		"Authorization": "Bearer secret-token",
		"X-Api-Key":     "sk-12345",
		"X-Custom":      "safe-value",
	}

	redacted := RedactConfig(cfg)
	if redacted.LLM.ExtraHeaders["Authorization"] != "***" {
		t.Errorf("Authorization should be redacted, got %q", redacted.LLM.ExtraHeaders["Authorization"])
	}
	if redacted.LLM.ExtraHeaders["X-Api-Key"] != "***" {
		t.Errorf("X-Api-Key should be redacted, got %q", redacted.LLM.ExtraHeaders["X-Api-Key"])
	}
	if redacted.LLM.ExtraHeaders["X-Custom"] != "safe-value" {
		t.Errorf("X-Custom should not be redacted, got %q", redacted.LLM.ExtraHeaders["X-Custom"])
	}
	// Original should not be modified.
	if cfg.LLM.ExtraHeaders["Authorization"] != "Bearer secret-token" {
		t.Error("original ExtraHeaders should not be modified")
	}
}

func TestLoadFromFileWithLLMNewFields(t *testing.T) {
	yamlContent := `
llm:
  provider: anthropic
  model: claude-sonnet-4-20250514
  api_key: file-key
  base_url: https://proxy.test
  pricing_file: /custom/pricing.json
  fallback_cost_per_1k: 0.01
  extra_headers:
    X-Custom: value
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.LLM.APIKey != "file-key" {
		t.Errorf("APIKey = %q, want file-key", cfg.LLM.APIKey)
	}
	if cfg.LLM.BaseURL != "https://proxy.test" {
		t.Errorf("BaseURL = %q, want https://proxy.test", cfg.LLM.BaseURL)
	}
	if cfg.LLM.PricingFile != "/custom/pricing.json" {
		t.Errorf("PricingFile = %q, want /custom/pricing.json", cfg.LLM.PricingFile)
	}
	if cfg.LLM.FallbackCostPer1K != 0.01 {
		t.Errorf("FallbackCostPer1K = %f, want 0.01", cfg.LLM.FallbackCostPer1K)
	}
	if cfg.LLM.ExtraHeaders["X-Custom"] != "value" {
		t.Errorf("ExtraHeaders[X-Custom] = %q, want value", cfg.LLM.ExtraHeaders["X-Custom"])
	}
}
