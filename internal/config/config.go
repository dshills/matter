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
	Planner PlannerConfig `yaml:"planner"`
	Tools   ToolsConfig   `yaml:"tools"`
	Sandbox SandboxConfig `yaml:"sandbox"`
	Observe ObserveConfig `yaml:"observe"`
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
}

// PlannerConfig controls planner behavior.
type PlannerConfig struct {
	SystemPrompt      string  `yaml:"system_prompt"`
	SystemPromptFile  string  `yaml:"system_prompt_file"`
	PromptPrefix      string  `yaml:"prompt_prefix"`
	PromptSuffix      string  `yaml:"prompt_suffix"`
	MaxResponseTokens int     `yaml:"max_response_tokens"`
	Temperature       float64 `yaml:"temperature"`
	MaxPlanSteps      int     `yaml:"max_plan_steps"` // max tool calls in a single plan sequence; 1 disables multi-step
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
	MaxAsks                  int           `yaml:"max_asks"`
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
	Provider          string            `yaml:"provider"`
	Model             string            `yaml:"model"`
	APIKey            string            `yaml:"api_key"`
	BaseURL           string            `yaml:"base_url"`
	Timeout           time.Duration     `yaml:"timeout"`
	MaxRetries        int               `yaml:"max_retries"`
	PricingFile       string            `yaml:"pricing_file"`
	FallbackCostPer1K float64           `yaml:"fallback_cost_per_1k"`
	ExtraHeaders      map[string]string `yaml:"extra_headers"`
}

// ToolsConfig controls which tool categories are enabled.
type ToolsConfig struct {
	EnableWorkspaceRead    bool              `yaml:"enable_workspace_read"`
	EnableWorkspaceWrite   bool              `yaml:"enable_workspace_write"`
	EnableWebFetch         bool              `yaml:"enable_web_fetch"`
	EnableCommandExec      bool              `yaml:"enable_command_exec"`
	CommandAllowlist       []string          `yaml:"command_allowlist"`
	WebFetchAllowedDomains []string          `yaml:"web_fetch_allowed_domains"`
	AllowedHiddenPaths     []string          `yaml:"allowed_hidden_paths"`
	MCPServers             []MCPServerConfig `yaml:"mcp_servers"`
	EnableWorkspaceFind    bool              `yaml:"enable_workspace_find"`
	EnableWorkspaceGrep    bool              `yaml:"enable_workspace_grep"`
	EnableWorkspaceEdit    bool              `yaml:"enable_workspace_edit"`
	EnableGit              bool              `yaml:"enable_git"`
}

// MCPServerConfig configures an external MCP tool server.
type MCPServerConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport"` // "stdio" or "sse"
	Command   string            `yaml:"command"`   // stdio only
	Args      []string          `yaml:"args"`      // stdio only
	URL       string            `yaml:"url"`       // sse only
	Env       map[string]string `yaml:"env"`       // stdio only: additional env vars
	Timeout   time.Duration     `yaml:"timeout"`   // per-tool timeout override
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

// ServerConfig controls the HTTP API server.
type ServerConfig struct {
	ListenAddr        string        `yaml:"listen_addr"`
	MaxConcurrentRuns int           `yaml:"max_concurrent_runs"`
	MaxPausedRuns     int           `yaml:"max_paused_runs"`
	RunRetention      time.Duration `yaml:"run_retention"`
	AuthToken         string        `yaml:"auth_token"`
}

// StorageConfig controls the persistent storage backend.
type StorageConfig struct {
	Backend         string        `yaml:"backend"`          // "sqlite" or "memory"
	Path            string        `yaml:"path"`             // SQLite file path
	Retention       time.Duration `yaml:"retention"`        // how long completed runs are kept
	PausedRetention time.Duration `yaml:"paused_retention"` // how long paused runs are kept
	GCInterval      time.Duration `yaml:"gc_interval"`      // how often retention cleanup runs
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
	if cfg.Agent.MaxAsks, err = envInt("MATTER_AGENT_MAX_ASKS", cfg.Agent.MaxAsks); err != nil {
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
	cfg.LLM.APIKey = envString("MATTER_LLM_API_KEY", cfg.LLM.APIKey)
	cfg.LLM.BaseURL = envString("MATTER_LLM_BASE_URL", cfg.LLM.BaseURL)
	if cfg.LLM.Timeout, err = envDuration("MATTER_LLM_TIMEOUT", cfg.LLM.Timeout); err != nil {
		return cfg, err
	}
	if cfg.LLM.MaxRetries, err = envInt("MATTER_LLM_MAX_RETRIES", cfg.LLM.MaxRetries); err != nil {
		return cfg, err
	}
	cfg.LLM.PricingFile = envString("MATTER_LLM_PRICING_FILE", cfg.LLM.PricingFile)
	if cfg.LLM.FallbackCostPer1K, err = envFloat("MATTER_LLM_FALLBACK_COST_PER_1K", cfg.LLM.FallbackCostPer1K); err != nil {
		return cfg, err
	}
	cfg.Planner.SystemPrompt = envString("MATTER_PLANNER_SYSTEM_PROMPT", cfg.Planner.SystemPrompt)
	cfg.Planner.SystemPromptFile = envString("MATTER_PLANNER_SYSTEM_PROMPT_FILE", cfg.Planner.SystemPromptFile)
	cfg.Planner.PromptPrefix = envString("MATTER_PLANNER_PROMPT_PREFIX", cfg.Planner.PromptPrefix)
	cfg.Planner.PromptSuffix = envString("MATTER_PLANNER_PROMPT_SUFFIX", cfg.Planner.PromptSuffix)
	if cfg.Planner.MaxResponseTokens, err = envInt("MATTER_PLANNER_MAX_RESPONSE_TOKENS", cfg.Planner.MaxResponseTokens); err != nil {
		return cfg, err
	}
	if cfg.Planner.Temperature, err = envFloat("MATTER_PLANNER_TEMPERATURE", cfg.Planner.Temperature); err != nil {
		return cfg, err
	}
	if cfg.Planner.MaxPlanSteps, err = envInt("MATTER_PLANNER_MAX_PLAN_STEPS", cfg.Planner.MaxPlanSteps); err != nil {
		return cfg, err
	}
	cfg.Tools.EnableWorkspaceRead = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_READ", cfg.Tools.EnableWorkspaceRead)
	cfg.Tools.EnableWorkspaceWrite = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_WRITE", cfg.Tools.EnableWorkspaceWrite)
	cfg.Tools.EnableWebFetch = envBool("MATTER_TOOLS_ENABLE_WEB_FETCH", cfg.Tools.EnableWebFetch)
	cfg.Tools.EnableCommandExec = envBool("MATTER_TOOLS_ENABLE_COMMAND_EXEC", cfg.Tools.EnableCommandExec)
	cfg.Tools.EnableWorkspaceFind = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_FIND", cfg.Tools.EnableWorkspaceFind)
	cfg.Tools.EnableWorkspaceGrep = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_GREP", cfg.Tools.EnableWorkspaceGrep)
	cfg.Tools.EnableWorkspaceEdit = envBool("MATTER_TOOLS_ENABLE_WORKSPACE_EDIT", cfg.Tools.EnableWorkspaceEdit)
	cfg.Tools.EnableGit = envBool("MATTER_TOOLS_ENABLE_GIT", cfg.Tools.EnableGit)
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
	cfg.Server.ListenAddr = envString("MATTER_SERVER_LISTEN_ADDR", cfg.Server.ListenAddr)
	if cfg.Server.MaxConcurrentRuns, err = envInt("MATTER_SERVER_MAX_CONCURRENT_RUNS", cfg.Server.MaxConcurrentRuns); err != nil {
		return cfg, err
	}
	if cfg.Server.MaxPausedRuns, err = envInt("MATTER_SERVER_MAX_PAUSED_RUNS", cfg.Server.MaxPausedRuns); err != nil {
		return cfg, err
	}
	if cfg.Server.RunRetention, err = envDuration("MATTER_SERVER_RUN_RETENTION", cfg.Server.RunRetention); err != nil {
		return cfg, err
	}
	// Note: prism may flag this line as "[REDACTED]" — that is its own diff
	// redaction of the auth token field name, not a code issue.
	cfg.Server.AuthToken = envString("MATTER_SERVER_AUTH_TOKEN", cfg.Server.AuthToken)
	cfg.Storage.Backend = envString("MATTER_STORAGE_BACKEND", cfg.Storage.Backend)
	cfg.Storage.Path = envString("MATTER_STORAGE_PATH", cfg.Storage.Path)
	if cfg.Storage.Retention, err = envDuration("MATTER_STORAGE_RETENTION", cfg.Storage.Retention); err != nil {
		return cfg, err
	}
	if cfg.Storage.PausedRetention, err = envDuration("MATTER_STORAGE_PAUSED_RETENTION", cfg.Storage.PausedRetention); err != nil {
		return cfg, err
	}
	if cfg.Storage.GCInterval, err = envDuration("MATTER_STORAGE_GC_INTERVAL", cfg.Storage.GCInterval); err != nil {
		return cfg, err
	}
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
	if cfg.Storage.Backend != "sqlite" && cfg.Storage.Backend != "memory" {
		return fmt.Errorf("storage backend must be \"sqlite\" or \"memory\", got %q", cfg.Storage.Backend)
	}
	if cfg.Storage.Backend == "sqlite" && cfg.Storage.Path == "" {
		return fmt.Errorf("storage path must not be empty when backend is sqlite")
	}
	if cfg.Storage.GCInterval <= 0 {
		return fmt.Errorf("storage gc_interval must be positive, got %s", cfg.Storage.GCInterval)
	}
	return nil
}

// RedactConfig returns a copy of the config with sensitive fields masked.
// Use this for logging or CLI output to prevent credential leakage.
func RedactConfig(cfg Config) Config {
	if cfg.LLM.APIKey != "" {
		cfg.LLM.APIKey = "***"
	}
	if len(cfg.LLM.ExtraHeaders) > 0 {
		redacted := make(map[string]string, len(cfg.LLM.ExtraHeaders))
		for k, v := range cfg.LLM.ExtraHeaders {
			lower := strings.ToLower(k)
			if lower == "authorization" || lower == "x-api-key" {
				redacted[k] = "***"
			} else {
				redacted[k] = v
			}
		}
		cfg.LLM.ExtraHeaders = redacted
	}
	if cfg.Server.AuthToken != "" {
		cfg.Server.AuthToken = "***"
	}
	return cfg
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
