package app_test

import (
	"testing"

	"github.com/Alex84K/sm_smart_home_tg/internal/telegram/app"
)

func TestMatchesRouting(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		kind     string
		want     bool
	}{
		{
			name:     "camera wildcard matches camera/offline",
			patterns: []string{"camera/*"},
			kind:     "camera/offline",
			want:     true,
		},
		{
			name:     "camera wildcard matches camera/online",
			patterns: []string{"camera/*"},
			kind:     "camera/online",
			want:     true,
		},
		{
			name:     "camera wildcard does not match sensor/offline",
			patterns: []string{"camera/*"},
			kind:     "sensor/offline",
			want:     false,
		},
		{
			name:     "camera wildcard does not match archive/error",
			patterns: []string{"camera/*"},
			kind:     "archive/error",
			want:     false,
		},
		{
			name:     "exact pattern matches exact kind",
			patterns: []string{"archive/error"},
			kind:     "archive/error",
			want:     true,
		},
		{
			name:     "exact pattern does not match other kind",
			patterns: []string{"archive/error"},
			kind:     "archive/recovered",
			want:     false,
		},
		{
			name:     "universal wildcard matches any event",
			patterns: []string{"*"},
			kind:     "sensor/temperature",
			want:     true,
		},
		{
			name:     "multiple patterns match if any matches",
			patterns: []string{"camera/*", "alert/*"},
			kind:     "alert/fired",
			want:     true,
		},
		{
			name:     "multiple patterns do not match outside category",
			patterns: []string{"camera/*", "alert/*"},
			kind:     "sensor/offline",
			want:     false,
		},
		{
			name:     "empty patterns match nothing",
			patterns: []string{},
			kind:     "camera/offline",
			want:     false,
		},
		{
			name:     "empty kind matches nothing",
			patterns: []string{"*"},
			kind:     "",
			want:     false,
		},
		{
			name:     "whitespace in pattern is trimmed",
			patterns: []string{" camera/* "},
			kind:     "camera/offline",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := app.MatchesRouting(tt.patterns, tt.kind)
			if got != tt.want {
				t.Errorf("MatchesRouting(%v, %q) = %v, want %v", tt.patterns, tt.kind, got, tt.want)
			}
		})
	}
}
