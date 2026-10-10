// Package maputil holds tiny generic helpers shared across pkg/engine's
// subpackages. internal/ scopes it to callers under pkg/engine.
package maputil

import "sort"

// SortedKeys returns m's keys in sorted order.
func SortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
