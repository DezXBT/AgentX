package xclient

import (
	"strconv"
)

// small dynamic-JSON helpers used by the parser. x.com responses are deeply
// nested and schema-unstable, so we navigate defensively rather than binding to
// rigid structs.

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// deepGet walks nested maps by string keys, returning nil on any miss.
func deepGet(v any, keys ...string) any {
	cur := v
	for _, k := range keys {
		m := asMap(cur)
		if m == nil {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// findKey recursively searches for the first value stored under key, doing a
// breadth-ish DFS. Used to locate "instructions" regardless of the timeline
// root, which differs per operation.
func findKey(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		if val, ok := t[key]; ok {
			return val
		}
		for _, child := range t {
			if r := findKey(child, key); r != nil {
				return r
			}
		}
	case []any:
		for _, child := range t {
			if r := findKey(child, key); r != nil {
				return r
			}
		}
	}
	return nil
}
