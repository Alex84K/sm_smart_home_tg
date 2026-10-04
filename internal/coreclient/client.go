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
	ErrCoreUnavailable = errors.New("core service unavailable")
	ErrCameraOffline   = errors.New("camera offline")
	ErrUnauthorized    = errors.New("unauthorized client token")
)

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
		Timeout: 10 * time.Second,
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
