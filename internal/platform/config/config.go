package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// TGConfig holds configuration for the Telegram gateway service.
type TGConfig struct {
	LogLevel         string
	TelegramBotToken string
	TelegramAPIURL   string
	AllowedIDs       []int64
	AlertChatID      int64
	CoreAPIURL       string
	CoreAPIToken     string
	MQTTURL          string
	MQTTUser         string
	MQTTPassword     string
	RoutingTelegram  []string
}

type yamlFile struct {
	Routing struct {
		Telegram []string `yaml:"telegram"`
	} `yaml:"routing"`
}

// LoadTGConfig loads and validates the Telegram gateway service configuration.
func LoadTGConfig(envPath string, yamlPath ...string) (*TGConfig, error) {
	loadEnv(envPath)

	cfg := &TGConfig{
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramAPIURL:   getEnv("TELEGRAM_API_URL", "https://api.telegram.org"),
		CoreAPIURL:       resolveCoreAPIURL(),
		CoreAPIToken:     os.Getenv("CORE_API_TOKEN"),
		MQTTURL:          getEnv("MQTT_URL", "tcp://mosquitto:1883"),
		MQTTUser:         getEnv("MQTT_GATEWAY_USER", getEnv("MQTT_USER", "tg-gateway")),
		MQTTPassword:     getEnv("MQTT_GATEWAY_PASSWORD", os.Getenv("MQTT_PASSWORD")),
	}

	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("config: missing required environment variable: TELEGRAM_BOT_TOKEN")
	}
	if cfg.CoreAPIToken == "" {
		return nil, fmt.Errorf("config: missing required environment variable: CORE_API_TOKEN")
	}
	if cfg.CoreAPIURL == "" {
		return nil, fmt.Errorf("config: missing required environment variable: CORE_API_URL")
	}

	rawAllowed := os.Getenv("TELEGRAM_ALLOWED_IDS")
	if strings.TrimSpace(rawAllowed) == "" {
		return nil, fmt.Errorf("config: missing required environment variable: TELEGRAM_ALLOWED_IDS")
	}

	allowedIDs, err := parseAllowedIDs(rawAllowed)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg.AllowedIDs = allowedIDs

	if rawAlert := os.Getenv("TELEGRAM_ALERT_CHAT_ID"); strings.TrimSpace(rawAlert) != "" {
		id, err := strconv.ParseInt(strings.TrimSpace(rawAlert), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("config: invalid TELEGRAM_ALERT_CHAT_ID: %q", rawAlert)
		}
		cfg.AlertChatID = id
	}

	yPath := "config.yaml"
	if len(yamlPath) > 0 && yamlPath[0] != "" {
		yPath = yamlPath[0]
	}

	if data, err := os.ReadFile(yPath); err == nil {
		var yf yamlFile
		if err := yaml.Unmarshal(data, &yf); err != nil {
			return nil, fmt.Errorf("config: parsing %s: %w", yPath, err)
		}
		cfg.RoutingTelegram = yf.Routing.Telegram
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: reading %s: %w", yPath, err)
	}

	if len(cfg.RoutingTelegram) == 0 {
		cfg.RoutingTelegram = []string{"camera/*"}
	}

	return cfg, nil
}

func loadEnv(path string) {
	if path != "" {
		_ = godotenv.Load(path)
	} else {
		_ = godotenv.Load(".env")
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func resolveCoreAPIURL() string {
	if url := os.Getenv("CORE_API_URL"); url != "" {
		return url
	}
	port := getEnv("CORE_PORT", getEnv("PORT", "8080"))
	return "http://core:" + strings.TrimPrefix(port, ":")
}

func parseAllowedIDs(raw string) ([]int64, error) {
	parts := strings.Split(raw, ",")
	var result []int64

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		id, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ID in TELEGRAM_ALLOWED_IDS: %q", trimmed)
		}
		result = append(result, id)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("TELEGRAM_ALLOWED_IDS contains no valid IDs")
	}

	return result, nil
}
