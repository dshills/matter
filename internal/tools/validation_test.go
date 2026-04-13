package tools

import "testing"

func TestValidateInputValid(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"path": {"type": "string"},
			"count": {"type": "integer"}
		},
		"required": ["path"]
	}`)
	input := map[string]any{
		"path":  "/tmp/file.txt",
		"count": 5,
	}
	if err := ValidateInput(schema, input); err != nil {
		t.Errorf("expected valid input: %v", err)
	}
}

func TestValidateInputMissingRequired(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"path": {"type": "string"}
		},
		"required": ["path"]
	}`)
	input := map[string]any{
		"other": "value",
	}
	if err := ValidateInput(schema, input); err == nil {
		t.Error("expected error for missing required field")
	}
}

func TestValidateInputWrongType(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"count": {"type": "integer"}
		}
	}`)
	input := map[string]any{
		"count": "not a number",
	}
	if err := ValidateInput(schema, input); err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestValidateInputEmptySchema(t *testing.T) {
	// Empty schema should accept anything.
	input := map[string]any{"key": "value"}
	if err := ValidateInput(nil, input); err != nil {
		t.Errorf("empty schema should accept any input: %v", err)
	}
	if err := ValidateInput([]byte{}, input); err != nil {
		t.Errorf("empty schema should accept any input: %v", err)
	}
}

func TestValidateInputEmptyObject(t *testing.T) {
	schema := []byte(`{"type": "object"}`)
	input := map[string]any{}
	if err := ValidateInput(schema, input); err != nil {
		t.Errorf("empty object should pass open schema: %v", err)
	}
}

func TestValidateInputAdditionalPropertiesFalse(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"additionalProperties": false
	}`)
	input := map[string]any{
		"name":    "test",
		"unknown": "field",
	}
	if err := ValidateInput(schema, input); err == nil {
		t.Error("expected error for additional properties")
	}
}

func TestValidateInputInvalidSchema(t *testing.T) {
	schema := []byte(`{not valid json}`)
	input := map[string]any{"key": "value"}
	if err := ValidateInput(schema, input); err == nil {
		t.Error("expected error for invalid schema")
	}
}
