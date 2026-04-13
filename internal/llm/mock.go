package llm

import (
	"context"
	"fmt"
	"sync"
)

// MockClient is a deterministic LLM client for testing.
// It returns predefined responses in sequence and records all requests.
type MockClient struct {
	mu        sync.Mutex
	responses []Response
	errors    []error
	requests  []Request
	index     int
}

// NewMockClient creates a mock client that returns the given responses in order.
// Each response can optionally have a corresponding error (pass nil for success).
// The responses and errors slices must have the same length.
func NewMockClient(responses []Response, errors []error) *MockClient {
	if errors == nil {
		errors = make([]error, len(responses))
	}
	if len(responses) != len(errors) {
		panic("MockClient: responses and errors slices must have the same length")
	}
	return &MockClient{
		responses: responses,
		errors:    errors,
	}
}

// Complete returns the next predefined response in sequence.
// Returns an error if all responses have been exhausted.
func (m *MockClient) Complete(_ context.Context, req Request) (Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, req)

	if m.index >= len(m.responses) {
		return Response{}, fmt.Errorf("mock client exhausted: %d responses provided, call %d", len(m.responses), m.index+1)
	}

	resp := m.responses[m.index]
	err := m.errors[m.index]
	m.index++
	return resp, err
}

// Requests returns all requests received by the mock client.
func (m *MockClient) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.requests))
	copy(out, m.requests)
	return out
}

// CallCount returns the number of Complete calls made.
func (m *MockClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}
