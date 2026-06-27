package adapter

import "strings"

// ExtractField extracts a value from a parsed JSON map using a dot-path expression.
// Paths like "$.customer_id" or "$.address.country_code" are supported.
func ExtractField(data map[string]any, path string) (any, bool) {
	if data == nil || path == "" {
		return nil, false
	}

	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	if path == "" {
		return nil, false
	}

	segments := strings.Split(path, ".")
	var current any = data

	for _, seg := range segments {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[seg]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

// ApplyFieldMapping applies all mappings to a parsed JSON response.
// Missing fields are omitted from the result.
func ApplyFieldMapping(data map[string]any, mapping map[string]string) map[string]any {
	result := make(map[string]any, len(mapping))
	for internalName, path := range mapping {
		if val, ok := ExtractField(data, path); ok {
			result[internalName] = val
		}
	}
	return result
}
