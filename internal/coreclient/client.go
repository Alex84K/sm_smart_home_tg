package coreclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
)

// Sentinel errors returned by the core client.
var (
	ErrCoreUnavailable  = errors.New("core service unavailable")
	ErrCameraOffline    = errors.New("camera offline")
	ErrUnauthorized     = errors.New("unauthorized client token")
	ErrRecorderDisabled = errors.New("recorder is disabled")
	ErrNoSegments       = errors.New("no segments available")
	ErrClipFailed       = errors.New("clip generation failed")
)

// ClipTooLargeError indicates requested clip duration exceeded size limit.
type ClipTooLargeError struct {
	Message           string
	MaxAllowedSeconds int
}

func (e *ClipTooLargeError) Error() string {
	return e.Message
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

	httpClient := &http.Client{
		Timeout: 60 * time.Second,
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

// GetClip fetches a video clip of the specified duration (seconds).
// If maxBytes > 0, it limits the clip size.
func (c *Client) GetClip(ctx context.Context, sec int, maxBytes int64) ([]byte, error) {
	params := &contract.GetClipParams{
		Sec: sec,
	}
	if maxBytes > 0 {
		params.MaxBytes = &maxBytes
	}

	resp, err := c.api.GetClipWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusConflict:
		return nil, ErrRecorderDisabled
	case http.StatusServiceUnavailable:
		if resp.JSON503 != nil && resp.JSON503.Error != "" {
			return nil, fmt.Errorf("%w: %s", ErrNoSegments, resp.JSON503.Error)
		}
		return nil, ErrNoSegments
	case http.StatusRequestEntityTooLarge:
		if resp.JSON413 != nil {
			return nil, &ClipTooLargeError{
				Message:           resp.JSON413.Error,
				MaxAllowedSeconds: resp.JSON413.MaxAllowedSeconds,
			}
		}
		return nil, &ClipTooLargeError{Message: "clip too large"}
	case http.StatusBadRequest:
		msg := "bad request"
		if resp.JSON400 != nil && resp.JSON400.Error != "" {
			msg = resp.JSON400.Error
		}
		return nil, errors.New(msg)
	case http.StatusInternalServerError:
		return nil, ErrClipFailed
	default:
		return nil, fmt.Errorf("%w: status %d", ErrCoreUnavailable, resp.StatusCode())
	}
}

// SetRecorder enables or disables the camera recorder.
func (c *Client) SetRecorder(ctx context.Context, enabled bool) (*contract.RecorderStatus, error) {
	resp, err := c.api.SetRecorderWithResponse(ctx, contract.RecorderRequest{
		Enabled: enabled,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		msg := fmt.Sprintf("status %d", resp.StatusCode())
		if resp.JSON400 != nil && resp.JSON400.Error != "" {
			msg = resp.JSON400.Error
		}
		return nil, fmt.Errorf("%w: %s", ErrCoreUnavailable, msg)
	}
}
