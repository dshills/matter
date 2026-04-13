package memory

import (
	"testing"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{"abcdefghi", 3},
	}
	for _, tt := range tests {
		got := EstimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msgs := []matter.Message{
		{Content: "abcd"},     // 1 token
		{Content: "abcdefgh"}, // 2 tokens
	}
	got := EstimateMessageTokens(msgs)
	if got != 3 {
		t.Errorf("EstimateMessageTokens = %d, want 3", got)
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		max       int
		want      string
		truncated bool
	}{
		{"no truncation", "hello", 10, "hello", false},
		{"exact limit", "hello", 5, "hello", false},
		{"truncated", "hello world", 5, "hello\n[TRUNCATED]", true},
		{"zero max", "hello", 0, "hello", false},
		{"negative max", "hello", -1, "hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateOutput(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("TruncateOutput(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

func TestStoreAddAndAll(t *testing.T) {
	s := NewStore()
	if s.Len() != 0 {
		t.Fatalf("new store should be empty, got %d", s.Len())
	}

	msgs := []matter.Message{
		{Role: matter.RoleSystem, Content: "sys"},
		{Role: matter.RoleUser, Content: "hello"},
		{Role: matter.RolePlanner, Content: "hi"},
	}
	for _, m := range msgs {
		s.Add(m)
	}

	if s.Len() != 3 {
		t.Fatalf("store len = %d, want 3", s.Len())
	}

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("All() len = %d, want 3", len(all))
	}

	// Verify order preserved.
	for i, m := range all {
		if m.Content != msgs[i].Content {
			t.Errorf("message %d content = %q, want %q", i, m.Content, msgs[i].Content)
		}
	}

	// Verify All returns a copy.
	all[0].Content = "mutated"
	original := s.All()
	if original[0].Content == "mutated" {
		t.Error("All() should return a copy, not a reference")
	}
}

func TestStoreSystemMessage(t *testing.T) {
	s := NewStore()

	// Empty store.
	_, err := s.SystemMessage()
	if err == nil {
		t.Error("expected error from empty store")
	}

	// Non-system first message.
	s.Add(matter.Message{Role: matter.RoleUser, Content: "user"})
	_, err = s.SystemMessage()
	if err == nil {
		t.Error("expected error when first message is not system")
	}

	// System first message.
	s2 := NewStore()
	sys := matter.Message{Role: matter.RoleSystem, Content: "system prompt"}
	s2.Add(sys)
	s2.Add(matter.Message{Role: matter.RoleUser, Content: "user"})

	got, err := s2.SystemMessage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "system prompt" {
		t.Errorf("SystemMessage content = %q, want %q", got.Content, "system prompt")
	}
}

func TestStoreNonSystemMessages(t *testing.T) {
	s := NewStore()

	// Empty store.
	if ns := s.NonSystemMessages(); ns != nil {
		t.Errorf("expected nil from empty store, got %v", ns)
	}

	// Only system message.
	s.Add(matter.Message{Role: matter.RoleSystem, Content: "sys"})
	if ns := s.NonSystemMessages(); ns != nil {
		t.Errorf("expected nil with only system message, got %v", ns)
	}

	// System + non-system.
	s.Add(matter.Message{Role: matter.RoleUser, Content: "a"})
	s.Add(matter.Message{Role: matter.RolePlanner, Content: "b"})

	ns := s.NonSystemMessages()
	if len(ns) != 2 {
		t.Fatalf("NonSystemMessages len = %d, want 2", len(ns))
	}
	if ns[0].Content != "a" || ns[1].Content != "b" {
		t.Errorf("NonSystemMessages = %v, want [a, b]", ns)
	}

	// Verify copy.
	ns[0].Content = "mutated"
	ns2 := s.NonSystemMessages()
	if ns2[0].Content == "mutated" {
		t.Error("NonSystemMessages should return a copy")
	}
}

func TestReplaceOldWithSummary(t *testing.T) {
	s := NewStore()
	s.Add(matter.Message{Role: matter.RoleSystem, Content: "sys"})
	s.Add(matter.Message{Role: matter.RoleUser, Content: "m1"})
	s.Add(matter.Message{Role: matter.RolePlanner, Content: "m2"})
	s.Add(matter.Message{Role: matter.RoleUser, Content: "m3"})
	s.Add(matter.Message{Role: matter.RolePlanner, Content: "m4"})
	s.Add(matter.Message{Role: matter.RoleUser, Content: "m5"})

	summary := matter.Message{Role: matter.RoleSystem, Content: "[Context Summary] summary"}

	// Keep 2 most recent non-system messages (m4, m5).
	s.ReplaceOldWithSummary(summary, 2)

	all := s.All()
	// Should be: system, summary, m4, m5
	if len(all) != 4 {
		t.Fatalf("after replace, len = %d, want 4", len(all))
	}
	if all[0].Content != "sys" {
		t.Errorf("index 0 should be system, got %q", all[0].Content)
	}
	if all[1].Content != "[Context Summary] summary" {
		t.Errorf("index 1 should be summary, got %q", all[1].Content)
	}
	if all[2].Content != "m4" {
		t.Errorf("index 2 should be m4, got %q", all[2].Content)
	}
	if all[3].Content != "m5" {
		t.Errorf("index 3 should be m5, got %q", all[3].Content)
	}
}

func TestReplaceOldWithSummaryNoOp(t *testing.T) {
	s := NewStore()
	s.Add(matter.Message{Role: matter.RoleSystem, Content: "sys"})
	s.Add(matter.Message{Role: matter.RoleUser, Content: "m1"})

	summary := matter.Message{Content: "summary"}
	// recentCount >= nonSystem count — should be a no-op.
	s.ReplaceOldWithSummary(summary, 5)

	if s.Len() != 2 {
		t.Errorf("should be no-op, len = %d, want 2", s.Len())
	}
}

func msg(role matter.MessageRole, content string) matter.Message {
	return matter.Message{Role: role, Content: content, Timestamp: time.Now()}
}
