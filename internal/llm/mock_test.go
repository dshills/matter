package llm

import (
	"context"
	"testing"

	"github.com/dshills/matter/pkg/matter"
)

func TestMockClientSequence(t *testing.T) {
	responses := []Response{
		{Content: "first", PromptTokens: 10, CompletionTokens: 5},
		{Content: "second", PromptTokens: 20, CompletionTokens: 10},
	}
	mock := NewMockClient(responses, nil)
	ctx := context.Background()

	r1, err := mock.Complete(ctx, Request{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Content != "first" {
		t.Errorf("got %q, want first", r1.Content)
	}

	r2, err := mock.Complete(ctx, Request{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Content != "second" {
		t.Errorf("got %q, want second", r2.Content)
	}
}

func TestMockClientExhausted(t *testing.T) {
	mock := NewMockClient([]Response{{Content: "only"}}, nil)
	ctx := context.Background()

	_, err := mock.Complete(ctx, Request{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mock.Complete(ctx, Request{})
	if err == nil {
		t.Error("expected error when responses exhausted")
	}
}

func TestMockClientWithErrors(t *testing.T) {
	responses := []Response{
		{},
		{Content: "recovered"},
	}
	errors := []error{
		context.DeadlineExceeded,
		nil,
	}
	mock := NewMockClient(responses, errors)
	ctx := context.Background()

	_, err := mock.Complete(ctx, Request{})
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	r, err := mock.Complete(ctx, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Content != "recovered" {
		t.Errorf("got %q, want recovered", r.Content)
	}
}

func TestMockClientRecordsRequests(t *testing.T) {
	mock := NewMockClient([]Response{{}, {}}, nil)
	ctx := context.Background()

	req1 := Request{
		Model:    "gpt-4o",
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "hello"}},
	}
	req2 := Request{
		Model:    "gpt-4o",
		Messages: []matter.Message{{Role: matter.RoleUser, Content: "world"}},
	}

	_, _ = mock.Complete(ctx, req1)
	_, _ = mock.Complete(ctx, req2)

	if mock.CallCount() != 2 {
		t.Errorf("call count = %d, want 2", mock.CallCount())
	}

	reqs := mock.Requests()
	if len(reqs) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(reqs))
	}
	if reqs[0].Messages[0].Content != "hello" {
		t.Errorf("first request content = %q, want hello", reqs[0].Messages[0].Content)
	}
	if reqs[1].Messages[0].Content != "world" {
		t.Errorf("second request content = %q, want world", reqs[1].Messages[0].Content)
	}
}

func TestMockClientCallCount(t *testing.T) {
	mock := NewMockClient(nil, nil)
	if mock.CallCount() != 0 {
		t.Error("call count should start at 0")
	}
}
