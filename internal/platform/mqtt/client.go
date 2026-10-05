package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// MessageHandler is called when a subscribed message arrives.
type MessageHandler = func(topic string, payload []byte)

type subscription struct {
	topic   string
	qos     byte
	handler MessageHandler
}

// Config holds MQTT client configuration for the gateway.
type Config struct {
	Broker            string
	ClientID          string
	Username          string
	Password          string
	CleanSession      bool
	KeepAlive         time.Duration
	PingTimeout       time.Duration
	ConnectTimeout    time.Duration
	ReconnectInterval time.Duration
	Logger            *slog.Logger
}

// Client wraps a paho MQTT client with background reconnection and subscription support.
type Client struct {
	pahoClient paho.Client
	cfg        Config
	log        *slog.Logger
	connected  atomic.Bool
	subsMu     sync.RWMutex
	subs       []subscription
}

// NewClient creates a new gateway MQTT Client with persistent session.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Broker == "" {
		return nil, fmt.Errorf("mqtt: missing broker URL")
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "tg-gateway"
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 30 * time.Second
	}
	if cfg.PingTimeout <= 0 {
		cfg.PingTimeout = 10 * time.Second
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.ReconnectInterval <= 0 {
		cfg.ReconnectInterval = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	c := &Client{
		cfg: cfg,
		log: cfg.Logger,
	}

	opts := paho.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetCleanSession(cfg.CleanSession)
	opts.SetResumeSubs(true)
	opts.SetKeepAlive(cfg.KeepAlive)
	opts.SetPingTimeout(cfg.PingTimeout)
	opts.SetConnectTimeout(cfg.ConnectTimeout)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(cfg.ReconnectInterval)
	opts.SetOrderMatters(false)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	// Messages queued in the persistent session arrive right after CONNACK, before
	// subscriptions are renewed, so they are dispatched by the default handler that
	// exists before Connect. Subscriptions carry no own callbacks.
	opts.SetDefaultPublishHandler(func(_ paho.Client, msg paho.Message) {
		c.dispatch(msg.Topic(), msg.Payload())
	})

	opts.SetOnConnectHandler(func(cl paho.Client) {
		c.connected.Store(true)
		c.log.Info("mqtt: connected to broker", "broker", cfg.Broker, "client_id", cfg.ClientID)

		c.subsMu.RLock()
		defer c.subsMu.RUnlock()
		for _, s := range c.subs {
			token := cl.Subscribe(s.topic, s.qos, nil)
			go func(t string) {
				if token.WaitTimeout(cfg.ConnectTimeout) && token.Error() != nil {
					c.log.Warn("mqtt: failed to subscribe on connect", "topic", t, "err", token.Error())
				} else {
					c.log.Info("mqtt: subscribed to topic", "topic", t)
				}
			}(s.topic)
		}
	})

	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		c.connected.Store(false)
		c.log.Warn("mqtt: connection lost", "broker", cfg.Broker, "err", err)
	})

	opts.SetReconnectingHandler(func(_ paho.Client, _ *paho.ClientOptions) {
		c.log.Debug("mqtt: attempting to reconnect", "broker", cfg.Broker)
	})

	c.pahoClient = paho.NewClient(opts)
	return c, nil
}

// Run manages initial connection and keeps running until ctx is canceled (run.Runner).
func (c *Client) Run(ctx context.Context) error {
	c.connectWithRetry(ctx)

	<-ctx.Done()
	c.connected.Store(false)
	c.pahoClient.Disconnect(250)
	c.log.Info("mqtt: disconnected from broker")
	return nil
}

func (c *Client) connectWithRetry(ctx context.Context) {
	token := c.pahoClient.Connect()
	if token.WaitTimeout(c.cfg.ConnectTimeout) && token.Error() == nil {
		return
	}

	err := token.Error()
	if err == nil {
		err = fmt.Errorf("connection timed out")
	}
	c.log.Warn("mqtt: broker unavailable on startup, retrying in background", "broker", c.cfg.Broker, "err", err)

	ticker := time.NewTicker(c.cfg.ReconnectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t := c.pahoClient.Connect()
			if t.WaitTimeout(c.cfg.ConnectTimeout) && t.Error() == nil {
				c.log.Info("mqtt: connected to broker after retry", "broker", c.cfg.Broker)
				return
			}
			c.log.Debug("mqtt: retry connection failed", "broker", c.cfg.Broker, "err", t.Error())
		}
	}
}

// IsConnected reports whether the client has an active broker connection.
func (c *Client) IsConnected() bool {
	return c.pahoClient.IsConnected()
}

// Subscribe registers a subscription to a topic with specified QoS and message handler.
// If connected, it immediately subscribes with the broker; if not yet connected,
// it will automatically subscribe when the connection is established.
func (c *Client) Subscribe(ctx context.Context, topic string, qos byte, handler MessageHandler) error {
	c.subsMu.Lock()
	c.subs = append(c.subs, subscription{topic: topic, qos: qos, handler: handler})
	c.subsMu.Unlock()

	if c.IsConnected() {
		token := c.pahoClient.Subscribe(topic, qos, nil)

		done := make(chan struct{})
		go func() {
			token.Wait()
			close(done)
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return token.Error()
		}
	}
	return nil
}

// dispatch delivers a message to the handlers of all subscriptions whose filter matches the topic.
func (c *Client) dispatch(topic string, payload []byte) {
	c.subsMu.RLock()
	defer c.subsMu.RUnlock()

	delivered := false
	for _, s := range c.subs {
		if topicMatches(s.topic, topic) {
			s.handler(topic, payload)
			delivered = true
		}
	}
	if !delivered {
		c.log.Warn("mqtt: message without matching subscription dropped", "topic", topic)
	}
}

// topicMatches reports whether topic matches an MQTT filter with "+" and "#" wildcards.
func topicMatches(filter, topic string) bool {
	fl := strings.Split(filter, "/")
	tl := strings.Split(topic, "/")
	for i, f := range fl {
		if f == "#" {
			return true
		}
		if i >= len(tl) {
			return false
		}
		if f != "+" && f != tl[i] {
			return false
		}
	}
	return len(fl) == len(tl)
}
