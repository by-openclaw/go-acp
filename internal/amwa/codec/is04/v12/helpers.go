package v12

import "encoding/json"

// marshalIndent is the canonical JSON serialiser used by every Encode
// method in this package — 2-space indent matches the formatting AMWA
// uses in spec annexes and the existing v1.3 codec, so fixture round-
// trips compare byte-exact.
func marshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// stripNestedKey walks a JSON object body to the leaf at `path`
// and deletes `key` — from each element when the leaf is an array,
// or from the object itself when it's a map. Idempotent on missing
// paths.
func stripNestedKey(raw []byte, path []string, key string) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	cur := v
	for _, p := range path[:len(path)-1] {
		m, ok := cur.(map[string]any)
		if !ok {
			return raw, nil
		}
		cur = m[p]
	}
	leaf, ok := cur.(map[string]any)
	if !ok {
		return raw, nil
	}
	switch t := leaf[path[len(path)-1]].(type) {
	case []any:
		for _, el := range t {
			if em, ok := el.(map[string]any); ok {
				delete(em, key)
			}
		}
	case map[string]any:
		delete(t, key)
	}
	return json.MarshalIndent(v, "", "  ")
}
