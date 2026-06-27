package adapter

import (
	"testing"
)

func TestExtractField(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]any
		path   string
		want   any
		wantOK bool
	}{
		{
			name:   "top-level string",
			data:   map[string]any{"customer_id": "C001"},
			path:   "$.customer_id",
			want:   "C001",
			wantOK: true,
		},
		{
			name:   "nested key",
			data:   map[string]any{"address": map[string]any{"country_code": "JP"}},
			path:   "$.address.country_code",
			want:   "JP",
			wantOK: true,
		},
		{
			name:   "deep nesting",
			data:   map[string]any{"a": map[string]any{"b": map[string]any{"c": 42.0}}},
			path:   "$.a.b.c",
			want:   42.0,
			wantOK: true,
		},
		{
			name:   "numeric value",
			data:   map[string]any{"amount": 1500.50},
			path:   "$.amount",
			want:   1500.50,
			wantOK: true,
		},
		{
			name:   "boolean value",
			data:   map[string]any{"active": true},
			path:   "$.active",
			want:   true,
			wantOK: true,
		},
		{
			name:   "path without dollar prefix",
			data:   map[string]any{"address": map[string]any{"country_code": "JP"}},
			path:   "address.country_code",
			want:   "JP",
			wantOK: true,
		},
		{
			name:   "missing top-level key",
			data:   map[string]any{"name": "test"},
			path:   "$.nonexistent",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "missing intermediate key",
			data:   map[string]any{"address": map[string]any{"city": "Tokyo"}},
			path:   "$.address.country_code",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "intermediate is not a map",
			data:   map[string]any{"address": "plain string"},
			path:   "$.address.country_code",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "nil intermediate",
			data:   map[string]any{"address": nil},
			path:   "$.address.country_code",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "empty path",
			data:   map[string]any{"key": "val"},
			path:   "",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "dollar only",
			data:   map[string]any{"key": "val"},
			path:   "$",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "nil data",
			data:   nil,
			path:   "$.key",
			want:   nil,
			wantOK: false,
		},
		{
			name:   "null value at key",
			data:   map[string]any{"key": nil},
			path:   "$.key",
			want:   nil,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractField(tt.data, tt.path)
			if ok != tt.wantOK {
				t.Errorf("ExtractField() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("ExtractField() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyFieldMapping(t *testing.T) {
	data := map[string]any{
		"customer_id": "C001",
		"full_name":   "Taro Yamada",
		"address":     map[string]any{"country_code": "JP"},
		"type":        "individual",
	}

	mapping := map[string]string{
		"external_id":   "$.customer_id",
		"name":          "$.full_name",
		"country":       "$.address.country_code",
		"customer_type": "$.type",
		"missing_field": "$.nonexistent",
	}

	result := ApplyFieldMapping(data, mapping)

	if result["external_id"] != "C001" {
		t.Errorf("external_id = %v, want C001", result["external_id"])
	}
	if result["name"] != "Taro Yamada" {
		t.Errorf("name = %v, want Taro Yamada", result["name"])
	}
	if result["country"] != "JP" {
		t.Errorf("country = %v, want JP", result["country"])
	}
	if result["customer_type"] != "individual" {
		t.Errorf("customer_type = %v, want individual", result["customer_type"])
	}
	if _, exists := result["missing_field"]; exists {
		t.Error("missing_field should not be in result")
	}
}

func TestApplyFieldMappingEmpty(t *testing.T) {
	result := ApplyFieldMapping(map[string]any{"key": "val"}, map[string]string{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}
