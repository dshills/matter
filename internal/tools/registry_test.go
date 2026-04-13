package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

func dummyTool(name string) matter.Tool {
	return matter.Tool{
		Name:        name,
		Description: "test tool " + name,
		InputSchema: []byte(`{"type":"object"}`),
		Timeout:     5 * time.Second,
		Execute: func(_ context.Context, _ map[string]any) (matter.ToolResult, error) {
			return matter.ToolResult{Output: "ok"}, nil
		},
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dummyTool("read")); err != nil {
		t.Fatal(err)
	}
	tool, ok := r.Get("read")
	if !ok {
		t.Fatal("expected tool to be found")
	}
	if tool.Name != "read" {
		t.Errorf("got name %q, want read", tool.Name)
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected tool not found")
	}
}

func TestRegistryDuplicateRejection(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dummyTool("read")); err != nil {
		t.Fatal(err)
	}
	err := r.Register(dummyTool("read"))
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistryListOrder(t *testing.T) {
	r := NewRegistry()
	names := []string{"alpha", "beta", "gamma"}
	for _, n := range names {
		if err := r.Register(dummyTool(n)); err != nil {
			t.Fatal(err)
		}
	}
	list := r.List()
	if len(list) != 3 {
		t.Fatalf("got %d tools, want 3", len(list))
	}
	for i, n := range names {
		if list[i].Name != n {
			t.Errorf("list[%d] = %q, want %q", i, list[i].Name, n)
		}
	}
}

func TestRegistryListReturnsCopy(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dummyTool("a")); err != nil {
		t.Fatal(err)
	}
	list := r.List()
	list[0].Name = "mutated"
	original, _ := r.Get("a")
	if original.Name != "a" {
		t.Error("List should return a copy, not a reference")
	}
}

func TestRegistrySchemas(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(dummyTool("read")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(dummyTool("write")); err != nil {
		t.Fatal(err)
	}

	data, err := r.Schemas()
	if err != nil {
		t.Fatal(err)
	}

	var schemas []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(data, &schemas); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("got %d schemas, want 2", len(schemas))
	}
	if schemas[0].Name != "read" {
		t.Errorf("first schema name = %q, want read", schemas[0].Name)
	}
	if schemas[1].Name != "write" {
		t.Errorf("second schema name = %q, want write", schemas[1].Name)
	}
}
