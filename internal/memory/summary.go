package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

const summaryPrompt = `Summarize the following conversation messages into a concise context summary.
Preserve:
- Factual intermediate results (tool outputs, data discovered)
- Outstanding goals and unresolved issues
- Tool failures relevant to future planning
- Budget and limit state if mentioned

Be concise but complete. Do not lose important factual information.`

// Summarize calls the LLM to produce a summary of the given messages.
// Returns the summary message and usage stats. The caller is responsible
// for counting the usage toward run limits.
func Summarize(ctx context.Context, client llm.Client, model string, messages []matter.Message) (matter.Message, SummarizationUsage, error) {
	var content strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&content, "[%s] %s\n", m.Role, m.Content)
	}

	req := llm.Request{
		Model: model,
		Messages: []matter.Message{
			{Role: matter.RoleSystem, Content: summaryPrompt, Timestamp: time.Now()},
			{Role: matter.RoleUser, Content: content.String(), Timestamp: time.Now()},
		},
		MaxTokens:   2000,
		Temperature: 0,
	}

	resp, err := client.Complete(ctx, req)
	if err != nil {
		return matter.Message{}, SummarizationUsage{}, fmt.Errorf("summarization LLM call failed: %w", err)
	}

	summaryMsg := matter.Message{
		Role:      matter.RoleSystem,
		Content:   fmt.Sprintf("[Context Summary]\n%s", resp.Content),
		Timestamp: time.Now(),
	}

	usage := SummarizationUsage{
		TotalTokens: resp.TotalTokens,
		CostUSD:     resp.EstimatedCostUSD,
	}

	return summaryMsg, usage, nil
}
