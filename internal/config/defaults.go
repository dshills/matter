package config

import "time"

// DefaultConfig returns a Config with all default values per spec Section 16.2.
func DefaultConfig() Config {
	return Config{
		Agent: AgentConfig{
			MaxSteps:                 20,
			MaxDuration:              2 * time.Minute,
			MaxPromptTokens:          40000,
			MaxCompletionTokens:      10000,
			MaxTotalTokens:           50000,
			MaxCostUSD:               3.00,
			MaxConsecutiveErrors:     3,
			MaxRepeatedToolCalls:     2,
			MaxConsecutiveNoProgress: 3,
			MaxAsks:                  3,
		},
		Memory: MemoryConfig{
			RecentMessages:         10,
			SummarizeAfterMessages: 15,
			SummarizeAfterTokens:   16000,
			SummarizationModel:     "gpt-4o-mini",
			MaxToolResultChars:     8000,
			MaxContextChars:        128000,
		},
		Planner: PlannerConfig{
			MaxResponseTokens: 4096,
			Temperature:       0,
		},
		LLM: LLMConfig{
			Provider:   "openai",
			Model:      "gpt-4o",
			Timeout:    30 * time.Second,
			MaxRetries: 3,
		},
		Tools: ToolsConfig{
			EnableWorkspaceRead:  true,
			EnableWorkspaceWrite: true,
			EnableWebFetch:       true,
			EnableCommandExec:    false,
		},
		Sandbox: SandboxConfig{
			CommandTimeout:      20 * time.Second,
			MemoryMB:            256,
			CPUShares:           1,
			NetworkEnabled:      false,
			MaxOutputBytes:      1048576, // 1 MB
			MaxWebResponseBytes: 524288,  // 512 KB
		},
		Observe: ObserveConfig{
			LogLevel:   "info",
			RecordRuns: true,
			RecordDir:  ".matter/runs",
		},
	}
}
