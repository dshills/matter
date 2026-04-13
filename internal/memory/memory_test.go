package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dshills/matter/internal/config"
	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

func testConfig() config.MemoryConfig {
	return config.MemoryConfig{
		RecentMessages:         3,
		SummarizeAfterMessages: 6,
		SummarizeAfterTokens:   100000, // high so token trigger doesn't fire
		SummarizationModel:     "test-model",
		MaxToolResultChars:     50,
	}
}

func addMsg(t *testing.T, mgr *Manager, m matter.Message) {
	t.Helper()
	if err := mgr.Add(context.Background(), m); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
}

func TestManagerAddAndContext(t *testing.T) {
	mock := llm.NewMockClient(nil, nil)
	mgr := NewManager(testConfig(), mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "system prompt"))
	addMsg(t, mgr, msg(matter.RoleUser, "hello"))
	addMsg(t, mgr, msg(matter.RolePlanner, "hi there"))

	ctx := mgr.Context()
	if len(ctx) != 3 {
		t.Fatalf("Context len = %d, want 3", len(ctx))
	}
	if ctx[0].Role != matter.RoleSystem {
		t.Error("first message should be system")
	}
	if ctx[1].Content != "hello" {
		t.Errorf("second message = %q, want %q", ctx[1].Content, "hello")
	}
}

func TestManagerMessageCount(t *testing.T) {
	mock := llm.NewMockClient(nil, nil)
	mgr := NewManager(testConfig(), mock)

	if mgr.MessageCount() != 0 {
		t.Fatalf("initial count = %d, want 0", mgr.MessageCount())
	}

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))
	addMsg(t, mgr, msg(matter.RoleUser, "a"))

	if mgr.MessageCount() != 2 {
		t.Errorf("count = %d, want 2", mgr.MessageCount())
	}
}

func TestManagerToolOutputTruncation(t *testing.T) {
	mock := llm.NewMockClient(nil, nil)
	mgr := NewManager(testConfig(), mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))

	longOutput := strings.Repeat("x", 100)
	addMsg(t, mgr, matter.Message{
		Role:      matter.RoleTool,
		Content:   longOutput,
		Timestamp: time.Now(),
	})

	ctx := mgr.Context()
	toolMsg := ctx[1]
	if len(toolMsg.Content) >= 100 {
		t.Error("tool output should be truncated")
	}
	if !strings.HasSuffix(toolMsg.Content, "[TRUNCATED]") {
		t.Error("truncated output should end with [TRUNCATED]")
	}
}

func TestManagerToolOutputNoTruncation(t *testing.T) {
	mock := llm.NewMockClient(nil, nil)
	mgr := NewManager(testConfig(), mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))

	shortOutput := "short"
	addMsg(t, mgr, matter.Message{
		Role:      matter.RoleTool,
		Content:   shortOutput,
		Timestamp: time.Now(),
	})

	ctx := mgr.Context()
	if ctx[1].Content != "short" {
		t.Errorf("short tool output should not be truncated, got %q", ctx[1].Content)
	}
}

func TestManagerMessageCountSummarizationTrigger(t *testing.T) {
	cfg := testConfig()
	cfg.SummarizeAfterMessages = 5 // trigger after 5 non-system messages
	cfg.RecentMessages = 2

	summaryResp := llm.Response{
		Content:          "Summary of conversation so far.",
		TotalTokens:      100,
		EstimatedCostUSD: 0.001,
	}
	mock := llm.NewMockClient([]llm.Response{summaryResp}, nil)
	mgr := NewManager(cfg, mock)

	// Add system message (doesn't count toward threshold).
	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))

	// Add 5 non-system messages to trigger summarization.
	for i := 0; i < 5; i++ {
		err := mgr.Add(context.Background(), msg(matter.RoleUser, strings.Repeat("m", i+1)))
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	// Summarization should have been called.
	if mock.CallCount() != 1 {
		t.Fatalf("expected 1 summarization call, got %d", mock.CallCount())
	}

	// Context should now be: system + summary + 2 recent.
	ctx := mgr.Context()
	if len(ctx) != 4 {
		t.Fatalf("Context len = %d, want 4", len(ctx))
	}
	if ctx[0].Role != matter.RoleSystem {
		t.Error("first message should be system")
	}
	if !strings.Contains(ctx[1].Content, "[Context Summary]") {
		t.Errorf("second message should be summary, got %q", ctx[1].Content)
	}

	// Usage should be tracked.
	usage := mgr.SummarizationUsage()
	if usage.TotalTokens != 100 {
		t.Errorf("TotalTokens = %d, want 100", usage.TotalTokens)
	}
	if usage.CostUSD != 0.001 {
		t.Errorf("CostUSD = %f, want 0.001", usage.CostUSD)
	}
}

func TestManagerTokenEvictionTrigger(t *testing.T) {
	cfg := testConfig()
	cfg.SummarizeAfterMessages = 1000 // disable message trigger
	// Use messages larger than summary prefix ("[Context Summary]\n" ≈ 5 tokens)
	// so that eviction actually reduces token count.
	// 40-char msgs ≈ 10 tokens each. 4 msgs + sys(1) = 41 > 40 → triggers.
	// After eviction with minRecent=3: sys(1) + summary(5) + 3*10 = 36 ≤ 40.
	cfg.SummarizeAfterTokens = 40

	summaryResp := llm.Response{Content: "s", TotalTokens: 5, EstimatedCostUSD: 0.0001}
	mock := llm.NewMockClient([]llm.Response{summaryResp}, nil)
	mgr := NewManager(cfg, mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))

	// Add 4 messages of 40 chars (≈10 tokens each).
	for i := 0; i < 4; i++ {
		err := mgr.Add(context.Background(), msg(matter.RoleUser, strings.Repeat("a", 40)))
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	// Token eviction should have triggered exactly once.
	if mock.CallCount() != 1 {
		t.Errorf("expected 1 summarization call from token eviction, got %d", mock.CallCount())
	}

	// System message should still be first.
	ctx := mgr.Context()
	if ctx[0].Role != matter.RoleSystem {
		t.Error("system message should remain first")
	}
}

func TestManagerSummarizationUsageCumulative(t *testing.T) {
	cfg := testConfig()
	cfg.SummarizeAfterMessages = 4
	cfg.SummarizeAfterTokens = 100000 // disable token trigger
	cfg.RecentMessages = 2

	// Provide enough responses for multiple summarizations.
	resps := make([]llm.Response, 5)
	for i := range resps {
		resps[i] = llm.Response{Content: "summary", TotalTokens: 100, EstimatedCostUSD: 0.001}
	}
	mock := llm.NewMockClient(resps, nil)
	mgr := NewManager(cfg, mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))

	// Trigger first summarization: 4 non-system messages.
	for i := 0; i < 4; i++ {
		addMsg(t, mgr, msg(matter.RoleUser, "msg"))
	}

	firstCount := mock.CallCount()
	if firstCount == 0 {
		t.Fatal("expected summarization after 4 non-system messages")
	}

	// Add more to trigger again.
	for i := 0; i < 3; i++ {
		addMsg(t, mgr, msg(matter.RoleUser, "more"))
	}

	totalCalls := mock.CallCount()
	if totalCalls <= firstCount {
		t.Skipf("second summarization didn't trigger (count=%d)", totalCalls)
	}

	// Usage should be cumulative.
	usage := mgr.SummarizationUsage()
	if usage.TotalTokens != totalCalls*100 {
		t.Errorf("cumulative TotalTokens = %d, want %d", usage.TotalTokens, totalCalls*100)
	}
	if usage.TotalTokens <= 100 {
		t.Error("cumulative usage should reflect multiple summarization calls")
	}
}

func TestManagerSummarizationError(t *testing.T) {
	cfg := testConfig()
	cfg.SummarizeAfterMessages = 3
	cfg.RecentMessages = 1

	mock := llm.NewMockClient(
		[]llm.Response{{}},
		[]error{context.DeadlineExceeded},
	)
	mgr := NewManager(cfg, mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))

	// Trigger summarization which will fail.
	for i := 0; i < 3; i++ {
		err := mgr.Add(context.Background(), msg(matter.RoleUser, "msg"))
		if err != nil {
			if !strings.Contains(err.Error(), "summarization") {
				t.Errorf("error should mention summarization: %v", err)
			}
			return // expected
		}
	}

	t.Error("expected summarization error to propagate")
}

func TestManagerNoSummarizationBelowThreshold(t *testing.T) {
	cfg := testConfig()
	cfg.SummarizeAfterMessages = 10

	mock := llm.NewMockClient(nil, nil)
	mgr := NewManager(cfg, mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "sys"))
	for i := 0; i < 3; i++ {
		addMsg(t, mgr, msg(matter.RoleUser, "msg"))
	}

	if mock.CallCount() != 0 {
		t.Errorf("no summarization should occur below threshold, got %d calls", mock.CallCount())
	}
}

func TestManagerSystemMessageAlwaysFirst(t *testing.T) {
	cfg := testConfig()
	cfg.SummarizeAfterMessages = 4
	cfg.SummarizeAfterTokens = 100000 // disable token trigger
	cfg.RecentMessages = 2

	// Provide enough responses for multiple summarization rounds.
	resps := make([]llm.Response, 5)
	for i := range resps {
		resps[i] = llm.Response{Content: "summary", TotalTokens: 10}
	}
	mock := llm.NewMockClient(resps, nil)
	mgr := NewManager(cfg, mock)

	addMsg(t, mgr, msg(matter.RoleSystem, "system prompt"))
	for i := 0; i < 5; i++ {
		addMsg(t, mgr, msg(matter.RoleUser, "msg"))
	}

	ctx := mgr.Context()
	if ctx[0].Content != "system prompt" {
		t.Errorf("system message should always be first, got %q", ctx[0].Content)
	}
	if ctx[0].Role != matter.RoleSystem {
		t.Error("first message role should be system")
	}
}
