package mqtt

import (
	"io"
	"log/slog"
	"testing"
)

func TestTopicMatches(t *testing.T) {
	cases := []struct {
		filter, topic string
		want          bool
	}{
		{"sh/events/#", "sh/events/camera/offline", true},
		{"sh/events/#", "sh/events", true},
		{"sh/events/#", "home/kitchen/temp", false},
		{"sh/events/+/offline", "sh/events/camera/offline", true},
		{"sh/events/+/offline", "sh/events/camera/online", false},
		{"sh/events/camera/offline", "sh/events/camera/offline", true},
		{"sh/events/camera", "sh/events/camera/offline", false},
		{"sh/events/camera/offline/x", "sh/events/camera/offline", false},
	}
	for _, tc := range cases {
		if got := topicMatches(tc.filter, tc.topic); got != tc.want {
			t.Errorf("topicMatches(%q, %q) = %v, want %v", tc.filter, tc.topic, got, tc.want)
		}
	}
}

// Messages from a persistent session can arrive before subscriptions are renewed;
// they must reach the handler registered via Subscribe.
func TestDispatchDeliversToRegisteredSubscription(t *testing.T) {
	c := &Client{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var got []string
	c.subs = []subscription{{
		topic:   "sh/events/#",
		qos:     1,
		handler: func(topic string, _ []byte) { got = append(got, topic) },
	}}

	c.dispatch("sh/events/camera/offline", []byte(`{}`))
	c.dispatch("home/kitchen/temp", []byte(`{}`))

	if len(got) != 1 || got[0] != "sh/events/camera/offline" {
		t.Fatalf("delivered topics = %v, want [sh/events/camera/offline]", got)
	}
}
