package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
)

// Sentinel errors returned by the core client.
var (
	ErrCoreUnavailable = errors.New("core service unavailable")
	ErrCameraOffline   = errors.New("camera offline")
	ErrUnauthorized    = errors.New("unauthorized client token")
	ErrClipBusy        = errors.New("clip recording already in progress")
	ErrClipNotFound    = errors.New("clip not found")
	ErrClipEmpty       = errors.New("clip is empty")
	ErrClipFailed      = errors.New("clip recording failed")
)

// ClipRecording is a clip recording started in the core.
type ClipRecording struct {
	ID          string
	MaxDuration time.Duration
}

// Client wraps the generated contract HTTP client with authentication and error handling.
type Client struct {
	api contract.ClientWithResponsesInterface
}

// New creates a new authenticated core client.
func New(baseURL, token string) (*Client, error) {
	bearerEditor := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	// Stopping a clip waits for ffmpeg to finish the file (up to 10 s in the core) and returns the video.
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	c, err := contract.NewClientWithResponses(baseURL,
		contract.WithHTTPClient(httpClient),
		contract.WithRequestEditorFn(bearerEditor),
	)
	if err != nil {
		return nil, fmt.Errorf("coreclient: %w", err)
	}

	return &Client{api: c}, nil
}

// NewWithInterface constructs a client with a custom or mock contract client.
func NewWithInterface(api contract.ClientWithResponsesInterface) *Client {
	return &Client{api: api}
}

// EventSubscriber defines the subscription interface needed to listen for events (ADR-0016).
type EventSubscriber interface {
	Subscribe(ctx context.Context, topic string, qos byte, handler func(topic string, payload []byte)) error
}

// EventHandler receives decoded EventEnvelopes.
type EventHandler func(event contract.EventEnvelope)

// SubscribeEvents subscribes to sh/events/# with QoS 1 and invokes handler for valid envelopes.
func (c *Client) SubscribeEvents(ctx context.Context, sub EventSubscriber, handler EventHandler) error {
	if sub == nil {
		return fmt.Errorf("coreclient: missing event subscriber")
	}
	return sub.Subscribe(ctx, "sh/events/#", 1, func(_ string, payload []byte) {
		var env contract.EventEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			return
		}
		handler(env)
	})
}

// GetPhoto fetches the latest camera JPEG photo.
func (c *Client) GetPhoto(ctx context.Context) ([]byte, error) {
	resp, err := c.api.GetPhotoWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusServiceUnavailable:
		if resp.JSON503 != nil && resp.JSON503.Error != "" {
			return nil, fmt.Errorf("%w: %s", ErrCameraOffline, resp.JSON503.Error)
		}
		return nil, ErrCameraOffline
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("%w: status %d", ErrCoreUnavailable, resp.StatusCode())
	}
}

// GetStatus fetches the current core subsystem status.
func (c *Client) GetStatus(ctx context.Context) (*contract.StatusResponse, error) {
	resp, err := c.api.GetStatusWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("%w: status %d", ErrCoreUnavailable, resp.StatusCode())
	}
}

// GetHealthz checks the liveness of the core service.
func (c *Client) GetHealthz(ctx context.Context) error {
	resp, err := c.api.GetHealthzWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrCoreUnavailable, resp.StatusCode())
	}
	return nil
}

// StartClip starts recording a clip in the core.
func (c *Client) StartClip(ctx context.Context) (*ClipRecording, error) {
	resp, err := c.api.StartClipWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return nil, fmt.Errorf("%w: empty start clip response", ErrCoreUnavailable)
		}
		return &ClipRecording{
			ID:          resp.JSON201.Id,
			MaxDuration: time.Duration(resp.JSON201.MaxSeconds) * time.Second,
		}, nil
	case http.StatusConflict:
		return nil, ErrClipBusy
	case http.StatusServiceUnavailable:
		return nil, ErrCameraOffline
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("%w: status %d", ErrCoreUnavailable, resp.StatusCode())
	}
}

// StopClip stops the clip recording and returns the mp4.
func (c *Client) StopClip(ctx context.Context, id string) ([]byte, error) {
	resp, err := c.api.StopClipWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusNotFound:
		return nil, ErrClipNotFound
	case http.StatusUnprocessableEntity:
		return nil, ErrClipEmpty
	case http.StatusInternalServerError:
		return nil, ErrClipFailed
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("%w: status %d", ErrCoreUnavailable, resp.StatusCode())
	}
}
