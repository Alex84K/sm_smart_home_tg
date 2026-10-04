package config_test

import (
	"strings"
	"testing"

	"github.com/Alex84K/tg_gateway_go/internal/platform/config"
)

func TestLoadTGConfigValidation(t *testing.T) {
	t.Run("missing TELEGRAM_BOT_TOKEN", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "")
		t.Setenv("CORE_API_TOKEN", "secret")
		t.Setenv("TELEGRAM_ALLOWED_IDS", "123,456")

		_, err := config.LoadTGConfig("/nonexistent")
		if err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
			t.Fatalf("expected error mentioning TELEGRAM_BOT_TOKEN, got: %v", err)
		}
	})

	t.Run("missing CORE_API_TOKEN", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "123:token")
		t.Setenv("CORE_API_TOKEN", "")
		t.Setenv("TELEGRAM_ALLOWED_IDS", "123,456")

		_, err := config.LoadTGConfig("/nonexistent")
		if err == nil || !strings.Contains(err.Error(), "CORE_API_TOKEN") {
			t.Fatalf("expected error mentioning CORE_API_TOKEN, got: %v", err)
		}
	})

	t.Run("invalid TELEGRAM_ALLOWED_IDS", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "123:token")
		t.Setenv("CORE_API_TOKEN", "secret")
		t.Setenv("TELEGRAM_ALLOWED_IDS", "123,abc")

		_, err := config.LoadTGConfig("/nonexistent")
		if err == nil || !strings.Contains(err.Error(), "TELEGRAM_ALLOWED_IDS") {
			t.Fatalf("expected error mentioning TELEGRAM_ALLOWED_IDS, got: %v", err)
		}
	})

	t.Run("valid tg config", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "123:token")
		t.Setenv("CORE_API_TOKEN", "secret")
		t.Setenv("TELEGRAM_ALLOWED_IDS", "123,456")

		cfg, err := config.LoadTGConfig("/nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.AllowedIDs) != 2 || cfg.AllowedIDs[0] != 123 || cfg.AllowedIDs[1] != 456 {
			t.Fatalf("unexpected allowed IDs: %v", cfg.AllowedIDs)
		}
		if cfg.CoreAPIURL != "http://core:8080" {
			t.Fatalf("expected default http://core:8080, got %s", cfg.CoreAPIURL)
		}
	})

	t.Run("resolves CoreAPIURL from CORE_PORT and PORT", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "123:token")
		t.Setenv("CORE_API_TOKEN", "secret")
		t.Setenv("TELEGRAM_ALLOWED_IDS", "123")

		t.Run("resolves from CORE_PORT", func(t *testing.T) {
			t.Setenv("CORE_API_URL", "")
			t.Setenv("CORE_PORT", "9090")
			cfg, err := config.LoadTGConfig("/nonexistent")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CoreAPIURL != "http://core:9090" {
				t.Fatalf("expected http://core:9090, got %s", cfg.CoreAPIURL)
			}
		})

		t.Run("resolves from PORT", func(t *testing.T) {
			t.Setenv("CORE_API_URL", "")
			t.Setenv("CORE_PORT", "")
			t.Setenv("PORT", "7070")
			cfg, err := config.LoadTGConfig("/nonexistent")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CoreAPIURL != "http://core:7070" {
				t.Fatalf("expected http://core:7070, got %s", cfg.CoreAPIURL)
			}
		})

		t.Run("explicit CORE_API_URL takes precedence", func(t *testing.T) {
			t.Setenv("CORE_PORT", "9090")
			t.Setenv("CORE_API_URL", "http://192.168.1.50:8000")
			cfg, err := config.LoadTGConfig("/nonexistent")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CoreAPIURL != "http://192.168.1.50:8000" {
				t.Fatalf("expected http://192.168.1.50:8000, got %s", cfg.CoreAPIURL)
			}
		})
	})
}
