package mqtt_test

import (
	"context"
	"testing"
	"time"

	"github.com/Alex84K/sm_smart_home_tg/internal/platform/mqtt"
)

func TestNewClientValidation(t *testing.T) {
	_, err := mqtt.NewClient(mqtt.Config{})
	if err == nil {
		t.Fatal("expected error for missing broker, got nil")
	}

	client, err := mqtt.NewClient(mqtt.Config{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "tg-gateway",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.IsConnected() {
		t.Error("expected client to not be connected initially")
	}
}

func TestRunBrokerUnavailableDoesNotCrash(t *testing.T) {
	client, err := mqtt.NewClient(mqtt.Config{
		Broker:            "tcp://127.0.0.1:1",
		ClientID:          "tg-gateway",
		ConnectTimeout:    50 * time.Millisecond,
		ReconnectInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected clean exit on context cancellation, got error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for Run to exit after context canceled")
	}
}
