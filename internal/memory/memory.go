package memory

import (
	"context"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// Manager manages context for the agent loop. It handles message storage,
// windowing, summarization triggers, and context building.
type Manager struct {
	store  *Store
	cfg    config.MemoryConfig
	client llm.Client // used for summarization calls
	usage  SummarizationUsage
}

// NewManager creates a memory manager with the given config and LLM client.
// The client is used for summarization calls (using the configured summarization model).
func NewManager(cfg config.MemoryConfig, client llm.Client) *Manager {
	return &Manager{
		store:  NewStore(),
		cfg:    cfg,
		client: client,
	}
}

// Add appends a message to the store and triggers summarization if thresholds are crossed.
func (m *Manager) Add(ctx context.Context, msg matter.Message) error {
	// Truncate tool outputs before storing for prompt use.
	if msg.Role == matter.RoleTool && m.cfg.MaxToolResultChars > 0 {
		msg.Content = TruncateOutput(msg.Content, m.cfg.MaxToolResultChars)
	}

	m.store.Add(msg)

	// Check message count trigger.
	nonSystemCount := m.store.Len() - 1 // exclude system message
	if nonSystemCount >= m.cfg.SummarizeAfterMessages {
		if err := m.summarizeOld(ctx); err != nil {
			return err
		}
	}

	// Check token count trigger.
	if m.estimatedTokens() > m.cfg.SummarizeAfterTokens {
		if err := m.evictByTokens(ctx); err != nil {
			return err
		}
	}

	return nil
}

// Context returns planner-ready messages: system message + optional summary + recent window.
func (m *Manager) Context() []matter.Message {
	return m.store.All()
}

// SummarizationUsage returns the cumulative token and cost usage from summarization calls.
func (m *Manager) SummarizationUsage() SummarizationUsage {
	return m.usage
}

// MessageCount returns the total message count (including system message).
func (m *Manager) MessageCount() int {
	return m.store.Len()
}

// summarizeOld replaces messages outside the recent window with a summary.
func (m *Manager) summarizeOld(ctx context.Context) error {
	nonSystem := m.store.NonSystemMessages()
	if len(nonSystem) <= m.cfg.RecentMessages {
		return nil
	}

	// Messages to summarize: everything outside the recent window.
	toSummarize := nonSystem[:len(nonSystem)-m.cfg.RecentMessages]

	summary, usage, err := Summarize(ctx, m.client, m.cfg.SummarizationModel, toSummarize)
	if err != nil {
		return err
	}

	m.usage.TotalTokens += usage.TotalTokens
	m.usage.CostUSD += usage.CostUSD

	m.store.ReplaceOldWithSummary(summary, m.cfg.RecentMessages)
	return nil
}

// evictByTokens shrinks the window until estimated tokens are below the threshold.
// Preserves the system message and at least 3 most recent non-system messages.
func (m *Manager) evictByTokens(ctx context.Context) error {
	const minRecent = 3

	for m.estimatedTokens() > m.cfg.SummarizeAfterTokens {
		nonSystem := m.store.NonSystemMessages()
		if len(nonSystem) <= minRecent {
			break // can't evict any more
		}

		// Evict the oldest non-system message outside the minimum window.
		keepCount := len(nonSystem) - 1
		if keepCount < minRecent {
			keepCount = minRecent
		}

		toSummarize := nonSystem[:len(nonSystem)-keepCount]
		if len(toSummarize) == 0 {
			break
		}

		summary, usage, err := Summarize(ctx, m.client, m.cfg.SummarizationModel, toSummarize)
		if err != nil {
			return err
		}

		m.usage.TotalTokens += usage.TotalTokens
		m.usage.CostUSD += usage.CostUSD

		m.store.ReplaceOldWithSummary(summary, keepCount)
	}

	return nil
}

// estimatedTokens returns the estimated total tokens across all stored messages.
func (m *Manager) estimatedTokens() int {
	return EstimateMessageTokens(m.store.All())
}
