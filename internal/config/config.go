package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for a matter run.
type Config struct {
	Agent   AgentConfig   `yaml:"agent"`
	Memory  MemoryConfig  `yaml:"memory"`
	LLM     LLMConfig     `yaml:"llm"`
	Tools   ToolsConfig   `yaml:"tools"`
	Sandbox SandboxConfig `yaml:"sandbox"`
	Observe ObserveConfig `yaml:"observe"`
}

// AgentConfig controls agent loop limits.
type AgentConfig struct {
	MaxSteps                 int           `yaml:"max_steps"`
	MaxDuration              time.Duration `yaml:"max_duration"`
	MaxPromptTokens          int           `yaml:"max_prompt_tokens"`
	MaxCompletionTokens      int           `yaml:"max_completion_tokens"`
	MaxTotalTokens           int           `yaml:"max_total_tokens"`
	MaxCostUSD               float64       `yaml:"max_cost_usd"`
	MaxConsecutiveErrors     int           `yaml:"max_consecutive_errors"`
	MaxRepeatedToolCalls     int           `yaml:"max_repeated_tool_calls"`
	MaxConsecutiveNoProgress int           `yaml:"max_consecutive_no_progress"`
}

// MemoryConfig controls context management.
type MemoryConfig struct {
	RecentMessages         int    `yaml:"recent_messages"`
	SummarizeAfterMessages int    `yaml:"summarize_after_messages"`
	SummarizeAfterTokens   int    `yaml:"summarize_after_tokens"`
	SummarizationModel     string `yaml:"summarization_model"`
	MaxToolResultChars     int    `yaml:"max_tool_result_chars"`
	MaxContextChars        int    `yaml:"max_context_chars"`
}

// LLMConfig controls the LLM provider.
type LLMConfig struct {
	Provider   string        `yaml:"provider"`
	Model      string        `yaml:"model"`
	Timeout    time.Duration `yaml:"timeout"`
	MaxRetries int           `yaml:"max_retries"`
}

// ToolsConfig controls which tool categories are enabled.
type ToolsConfig struct {
	EnableWorkspaceRead    bool     `yaml:"enable_workspace_read"`
	EnableWorkspaceWrite   bool     `yaml:"enable_workspace_write"`
	EnableWebFetch         bool     `yaml:"enable_web_fetch"`
	EnableCommandExec      bool     `yaml:"enable_command_exec"`
	WebFetchAllowedDomains []string `yaml:"web_fetch_allowed_domains"`
	AllowedHiddenPaths     []string `yaml:"allowed_hidden_paths"`
}

// SandboxConfig controls resource limits for command execution.
type SandboxConfig struct {
	CommandTimeout      time.Duration `yaml:"command_timeout"`
	MemoryMB            int           `yaml:"memory_mb"`
	CPUShares           int           `yaml:"cpu_shares"`
	NetworkEnabled      bool          `yaml:"network_enabled"`
	MaxOutputBytes      int           `yaml:"max_output_bytes"`
	MaxWebResponseBytes int           `yaml:"max_web_response_bytes"`
}

// ObserveConfig controls logging and run recording.
type ObserveConfig struct {
	LogLevel   string `yaml:"log_level"`
	RecordRuns bool   `yaml:"record_runs"`
	RecordDir  string `yaml:"record_dir"`
}

// LoadFromFile reads a YAML config file and merges it over defaults.
func LoadFromFile(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config file: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config file: %w", err)
	}
	return cfg, nil
}

// ApplyEnv overlays environment variables onto the config.
// Environment variables use the MATTER_ prefix with uppercase snake_case.
// Returns an error if any set environment variable has a malformed value.
func ApplyEnv(cfg Config) (Config, error) {
	var err error

	if cfg.Agent.MaxSteps, err = envInt("MATTER_AGENT_MAX_STEPS", cfg.Agent.MaxSteps); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxDuration, err = envDuration("MATTER_AGENT_MAX_DURATION", cfg.Agent.MaxDuration); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxPromptTokens, err = envInt("MATTER_AGENT_MAX_PROMPT_TOKENS", cfg.Agent.MaxPromptTokens); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxCompletionTokens, err = envInt("MATTER_AGENT_MAX_COMPLETION_TOKENS", cfg.Agent.MaxCompletionTokens); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxTotalTokens, err = envInt("MATTER_AGENT_MAX_TOTAL_TOKENS", cfg.Agent.MaxTotalTokens); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxCostUSD, err = envFloat("MATTER_AGENT_MAX_COST_USD", cfg.Agent.MaxCostUSD); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxConsecutiveErrors, err = envInt("MATTER_AGENT_MAX_CONSECUTIVE_ERRORS", cfg.Agent.MaxConsecutiveErrors); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxRepeatedToolCalls, err = envInt("MATTER_AGENT_MAX_REPEATED_TOOL_CALLS", cfg.Agent.MaxRepeatedToolCalls); err != nil {
		return cfg, err
	}
	if cfg.Agent.MaxConsecutiveNoProgress, err = envInt("MATTER_AGENT_MAX_CONSECUTIVE_NO_PROGRESS", cfg.Agent.MaxConsecutiveNoProgress); err != nil {
		return cfg, err
	}
	if cfg.Memory.RecentMessages, err = envInt("MATTER_MEMORY_RECENT_MESSAGES", cfg.Memory.RecentMessages); err != nil {
		return cfg, err
	}
	if cfg.Memory.SummarizeAfterMessages, err = envInt("MATTER_MEMORY_SUMMARIZE_AFTER_MESSAGES", cfg.Memory.SummarizeAfterMessages); err != nil {
		return cfg, err
	}
	if cfg.Memory.SummarizeAfterTokens, err = envInt("MATTER_MEMORY_SUMMARIZE_AFTER_TOKENS", cfg.Memory.SummarizeAfterTokens); err != nil {
		return cfg, err
	}
	cfg.Memory.SummarizationModel = envString("MATTER_MEMORY_SUMMARIZATION_MODEL", cfg.Memory.SummarizationModel)
	if cfg.Memory.MaxToolResultChars, err = envInt("MATTER_MEMORY_MAX_TOOL_RESULT_CHARS", cfg.Memory.MaxToolResultChars); err != nil {
		return cfg, err
	}
	if cfg.Memory.MaxContextChars, err = envInt("MATTER_MEMORY_MAX_CONTEXT_CHARS", cfg.Memory.MaxContextChars); err != nil {
		return cfg, err
	}
	cfg.LLM.Provider = envString("MATTER_LLM_PROVIDER", cfg.LLM.Provider)
	cfg.LLM.Model = envString("MATTER_LLM_MODEL", cfg.LLM.Model)
	if cfg.LLM.Timeout, err = envDuration("MATTER_LLM_TIMEOUT", cfg.LLM.Timeout); err != nil {
		return cfg, err
	}
	if cfg.LLM.MaxRetries, err = envInt("MATTER_LLM_MAX_RETRIES", cfg.LLM.MaxRetries); err != nil {
		return cfg, err
	}
	cfg.Tools.EnableWorkspaceRead = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_READ", cfg.Tools.EnableWorkspaceRead)
	cfg.Tools.EnableWorkspaceWrite = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_WRITE", cfg.Tools.EnableWorkspaceWrite)
	cfg.Tools.EnableWebFetch = envBool("MATTER_TOOLS_ENABLE_WEB_FETCH", cfg.Tools.EnableWebFetch)
	cfg.Tools.EnableCommandExec = envBool("MATTER_TOOLS_ENABLE_COMMAND_EXEC", cfg.Tools.EnableCommandExec)
	if cfg.Sandbox.CommandTimeout, err = envDuration("MATTER_SANDBOX_COMMAND_TIMEOUT", cfg.Sandbox.CommandTimeout); err != nil {
		return cfg, err
	}
	if cfg.Sandbox.MemoryMB, err = envInt("MATTER_SANDBOX_MEMORY_MB", cfg.Sandbox.MemoryMB); err != nil {
		return cfg, err
	}
	if cfg.Sandbox.CPUShares, err = envInt("MATTER_SANDBOX_CPU_SHARES", cfg.Sandbox.CPUShares); err != nil {
		return cfg, err
	}
	cfg.Sandbox.NetworkEnabled = envBool("MATTER_SANDBOX_NETWORK_ENABLED", cfg.Sandbox.NetworkEnabled)
	if cfg.Sandbox.MaxOutputBytes, err = envInt("MATTER_SANDBOX_MAX_OUTPUT_BYTES", cfg.Sandbox.MaxOutputBytes); err != nil {
		return cfg, err
	}
	if cfg.Sandbox.MaxWebResponseBytes, err = envInt("MATTER_SANDBOX_MAX_WEB_RESPONSE_BYTES", cfg.Sandbox.MaxWebResponseBytes); err != nil {
		return cfg, err
	}
	cfg.Observe.LogLevel = envString("MATTER_OBSERVE_LOG_LEVEL", cfg.Observe.LogLevel)
	cfg.Observe.RecordRuns = envBool("MATTER_OBSERVE_RECORD_RUNS", cfg.Observe.RecordRuns)
	cfg.Observe.RecordDir = envString("MATTER_OBSERVE_RECORD_DIR", cfg.Observe.RecordDir)
	return cfg, nil
}

// Validate checks configuration invariants and returns a configuration error
// if any are violated.
func Validate(cfg Config) error {
	if cfg.Memory.SummarizeAfterMessages <= cfg.Memory.RecentMessages {
		return fmt.Errorf("summarize_after_messages (%d) must be greater than recent_messages (%d)",
			cfg.Memory.SummarizeAfterMessages, cfg.Memory.RecentMessages)
	}
	if cfg.Agent.MaxSteps < 1 {
		return fmt.Errorf("max_steps must be at least 1, got %d", cfg.Agent.MaxSteps)
	}
	if cfg.Agent.MaxDuration < time.Second {
		return fmt.Errorf("max_duration must be at least 1s, got %s", cfg.Agent.MaxDuration)
	}
	if cfg.LLM.Timeout < time.Second {
		return fmt.Errorf("llm timeout must be at least 1s, got %s", cfg.LLM.Timeout)
	}
	if cfg.LLM.Provider == "" {
		return fmt.Errorf("llm provider must not be empty")
	}
	if cfg.LLM.Model == "" {
		return fmt.Errorf("llm model must not be empty")
	}
	return nil
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return n, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return f, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return d, nil
}

func envString(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "true" || v == "1" || v == "yes"
}
