package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dshills/matter/internal/errtype"
)

func TestRetryClientSuccessNoRetry(t *testing.T) {
	mock := NewMockClient([]Response{{Content: "ok"}}, nil)
	client := NewRetryClient(mock, 3)

	resp, err := client.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("got %q, want ok", resp.Content)
	}
	if mock.CallCount() != 1 {
		t.Errorf("should call once on success, got %d", mock.CallCount())
	}
}

func TestRetryClientRetriesOnTransient(t *testing.T) {
	responses := []Response{{}, {}, {Content: "recovered"}}
	errs := []error{
		errors.New("transient failure"),
		errors.New("transient failure again"),
		nil,
	}
	mock := NewMockClient(responses, errs)

	client := NewRetryClient(mock, 3)
	client.baseDelay = time.Millisecond // speed up test

	resp, err := client.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatalf("expected recovery after retries: %v", err)
	}
	if resp.Content != "recovered" {
		t.Errorf("got %q, want recovered", resp.Content)
	}
	if mock.CallCount() != 3 {
		t.Errorf("expected 3 calls, got %d", mock.CallCount())
	}
}

func TestRetryClientNoRetryOnTerminal(t *testing.T) {
	terminalErr := errtype.NewLLMError("auth failed", nil, false) // false = terminal
	mock := NewMockClient([]Response{{}}, []error{terminalErr})
	client := NewRetryClient(mock, 3)
	client.baseDelay = time.Millisecond

	_, err := client.Complete(context.Background(), Request{})
	if err == nil {
		t.Error("expected terminal error to propagate")
	}
	if mock.CallCount() != 1 {
		t.Errorf("terminal error should not retry, got %d calls", mock.CallCount())
	}
}

func TestRetryClientRetriesOnRetriableAgentError(t *testing.T) {
	retriableErr := errtype.NewLLMError("timeout", nil, true) // true = retriable
	responses := []Response{{}, {Content: "ok"}}
	errs := []error{retriableErr, nil}
	mock := NewMockClient(responses, errs)

	client := NewRetryClient(mock, 3)
	client.baseDelay = time.Millisecond

	resp, err := client.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("got %q, want ok", resp.Content)
	}
	if mock.CallCount() != 2 {
		t.Errorf("expected 2 calls, got %d", mock.CallCount())
	}
}

func TestRetryClientExhaustsRetries(t *testing.T) {
	responses := []Response{{}, {}, {}}
	errs := []error{
		errors.New("fail 1"),
		errors.New("fail 2"),
		errors.New("fail 3"),
	}
	mock := NewMockClient(responses, errs)

	client := NewRetryClient(mock, 2) // 1 initial + 2 retries = 3 attempts
	client.baseDelay = time.Millisecond

	_, err := client.Complete(context.Background(), Request{})
	if err == nil {
		t.Error("expected error after exhausting retries")
	}
	if mock.CallCount() != 3 {
		t.Errorf("expected 3 total attempts (1 + 2 retries), got %d", mock.CallCount())
	}
}

func TestRetryClientRespectsContextCancellation(t *testing.T) {
	responses := []Response{{}, {}}
	errs := []error{errors.New("fail"), nil}
	mock := NewMockClient(responses, errs)

	client := NewRetryClient(mock, 3)
	client.baseDelay = time.Second // long delay

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, Request{})
	if err == nil {
		t.Error("expected context error")
	}
}

func TestRetryClientNoRetryOnContextCanceled(t *testing.T) {
	mock := NewMockClient([]Response{{}}, []error{context.Canceled})
	client := NewRetryClient(mock, 3)
	client.baseDelay = time.Millisecond

	_, err := client.Complete(context.Background(), Request{})
	if err == nil {
		t.Error("expected context.Canceled to propagate")
	}
	if mock.CallCount() != 1 {
		t.Errorf("context.Canceled should not retry, got %d calls", mock.CallCount())
	}
}

func TestRetryClientZeroRetries(t *testing.T) {
	mock := NewMockClient([]Response{{}}, []error{errors.New("fail")})
	client := NewRetryClient(mock, 0) // no retries

	_, err := client.Complete(context.Background(), Request{})
	if err == nil {
		t.Error("expected error with 0 retries")
	}
	if mock.CallCount() != 1 {
		t.Errorf("with 0 retries should call once, got %d", mock.CallCount())
	}
}
