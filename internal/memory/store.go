// Package memory provides context management with windowing and summarization.
package memory

import (
	"fmt"

	"github.com/dshills/matter/pkg/matter"
)

// EstimateTokens returns a rough token count for a string using chars/4 heuristic.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4 // ceiling division
}

// EstimateMessageTokens returns the estimated token count for a slice of messages.
func EstimateMessageTokens(msgs []matter.Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content)
	}
	return total
}

// TruncateOutput truncates a tool output to maxChars and appends a notice.
func TruncateOutput(output string, maxChars int) string {
	if maxChars <= 0 || len(output) <= maxChars {
		return output
	}
	return output[:maxChars] + "\n[TRUNCATED]"
}

// SummarizationUsage tracks token and cost usage from summarization calls.
type SummarizationUsage struct {
	TotalTokens int
	CostUSD     float64
}

// Store holds all messages for a run. It maintains insertion order and
// provides access to the system message, which is always at index 0.
type Store struct {
	messages []matter.Message
}

// NewStore creates an empty message store.
func NewStore() *Store {
	return &Store{}
}

// Add appends a message to the store.
func (s *Store) Add(msg matter.Message) {
	s.messages = append(s.messages, msg)
}

// All returns all messages in insertion order.
func (s *Store) All() []matter.Message {
	out := make([]matter.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// Len returns the number of messages.
func (s *Store) Len() int {
	return len(s.messages)
}

// SystemMessage returns the first system message, or an error if none exists.
func (s *Store) SystemMessage() (matter.Message, error) {
	if len(s.messages) == 0 {
		return matter.Message{}, fmt.Errorf("no messages in store")
	}
	if s.messages[0].Role != matter.RoleSystem {
		return matter.Message{}, fmt.Errorf("first message is not a system message")
	}
	return s.messages[0], nil
}

// NonSystemMessages returns all messages except the system message (index 0).
func (s *Store) NonSystemMessages() []matter.Message {
	if len(s.messages) <= 1 {
		return nil
	}
	out := make([]matter.Message, len(s.messages)-1)
	copy(out, s.messages[1:])
	return out
}

// ReplaceOldWithSummary replaces messages outside the recent window with a
// summary message. Keeps the system message (index 0) and the last
// recentCount non-system messages. All messages between are replaced with
// the summary.
func (s *Store) ReplaceOldWithSummary(summary matter.Message, recentCount int) {
	nonSystem := len(s.messages) - 1 // exclude system message
	if nonSystem <= recentCount {
		return // nothing to replace
	}

	// Keep: [system] + [summary] + [last recentCount messages]
	recentStart := len(s.messages) - recentCount
	recent := make([]matter.Message, recentCount)
	copy(recent, s.messages[recentStart:])

	newMessages := make([]matter.Message, 0, 2+recentCount)
	newMessages = append(newMessages, s.messages[0]) // system
	newMessages = append(newMessages, summary)
	newMessages = append(newMessages, recent...)
	s.messages = newMessages
}
