package app

import "strings"

// MatchesRouting reports whether kind matches any of the given routing patterns (ADR-0004).
// Supported patterns:
// - "*" matches everything
// - "<prefix>/*" matches any kind starting with "<prefix>/" (e.g. "camera/*" matches "camera/offline")
// - exact match (e.g. "archive/error" matches "archive/error")
func MatchesRouting(patterns []string, kind string) bool {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return false
	}

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == kind {
			return true
		}
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(kind, prefix) {
				return true
			}
		}
	}

	return false
}
