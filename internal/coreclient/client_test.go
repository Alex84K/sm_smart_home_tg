package coreclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
	"github.com/Alex84K/sm_smart_home_tg/internal/coreclient"
)

func TestCoreClient(t *testing.T) {
	var authHeaderReceived string
	var returnStatus int
	var responseBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderReceived = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/photo" && returnStatus == http.StatusOK {
			w.Header().Set("Content-Type", "image/jpeg")
		}
		w.WriteHeader(returnStatus)
		_, _ = w.Write(responseBody)
	}))
	defer ts.Close()

	client, err := coreclient.New(ts.URL, "test-token")
	if err != nil {
		t.Fatalf("coreclient.New failed: %v", err)
	}

	ctx := context.Background()

	t.Run("GetPhoto success", func(t *testing.T) {
		returnStatus = http.StatusOK
		responseBody = []byte{0xFF, 0xD8, 0x01, 0xFF, 0xD9}

		photo, err := client.GetPhoto(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(photo) != 5 {
			t.Fatalf("expected 5 bytes, got %d", len(photo))
		}
		if authHeaderReceived != "Bearer test-token" {
			t.Fatalf("expected Authorization Bearer test-token, got %q", authHeaderReceived)
		}
	})

	t.Run("GetPhoto camera offline 503", func(t *testing.T) {
		returnStatus = http.StatusServiceUnavailable
		errResp, _ := json.Marshal(contract.ErrorResponse{Error: "camera offline"})
		responseBody = errResp

		_, err := client.GetPhoto(ctx)
		if !errors.Is(err, coreclient.ErrCameraOffline) {
			t.Fatalf("expected ErrCameraOffline, got %v", err)
		}
	})

	t.Run("GetStatus success", func(t *testing.T) {
		returnStatus = http.StatusOK
		stResp, _ := json.Marshal(contract.StatusResponse{
			Camera: contract.CameraStatus{Online: true},
			Storage: contract.StorageStatus{
				FreeBytes:  1024,
				TotalBytes: 2048,
			},
		})
		responseBody = stResp

		st, err := client.GetStatus(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !st.Camera.Online {
			t.Fatal("expected camera online")
		}
		if st.Storage.FreeBytes != 1024 {
			t.Fatalf("expected 1024 free bytes, got %d", st.Storage.FreeBytes)
		}
	})

	t.Run("Unauthorized 401", func(t *testing.T) {
		returnStatus = http.StatusUnauthorized
		errResp, _ := json.Marshal(contract.ErrorResponse{Error: "unauthorized"})
		responseBody = errResp

		_, err := client.GetStatus(ctx)
		if !errors.Is(err, coreclient.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized, got %v", err)
		}
	})

	t.Run("Core down", func(t *testing.T) {
		deadClient, _ := coreclient.New("http://127.0.0.1:54321", "token")
		_, err := deadClient.GetStatus(ctx)
		if !errors.Is(err, coreclient.ErrCoreUnavailable) {
			t.Fatalf("expected ErrCoreUnavailable, got %v", err)
		}
	})

	t.Run("GetClip success", func(t *testing.T) {
		returnStatus = http.StatusOK
		responseBody = []byte("mp4-data")

		clip, err := client.GetClip(ctx, 30, 50*1024*1024)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(clip) != "mp4-data" {
			t.Fatalf("expected mp4-data, got %s", string(clip))
		}
	})

	t.Run("GetClip recorder disabled 409", func(t *testing.T) {
		returnStatus = http.StatusConflict
		errResp, _ := json.Marshal(contract.ErrorResponse{Error: "recorder disabled"})
		responseBody = errResp

		_, err := client.GetClip(ctx, 30, 0)
		if !errors.Is(err, coreclient.ErrRecorderDisabled) {
			t.Fatalf("expected ErrRecorderDisabled, got %v", err)
		}
	})

	t.Run("GetClip no segments 503", func(t *testing.T) {
		returnStatus = http.StatusServiceUnavailable
		errResp, _ := json.Marshal(contract.ErrorResponse{Error: "no segments available"})
		responseBody = errResp

		_, err := client.GetClip(ctx, 30, 0)
		if !errors.Is(err, coreclient.ErrNoSegments) {
			t.Fatalf("expected ErrNoSegments, got %v", err)
		}
	})

	t.Run("GetClip clip too large 400", func(t *testing.T) {
		returnStatus = http.StatusBadRequest
		errResp, _ := json.Marshal(contract.ErrorResponse{
			Error: "media: clip size 60000000 bytes exceeds max_bytes 50000000: max allowed duration is ~25 seconds",
		})
		responseBody = errResp

		_, err := client.GetClip(ctx, 30, 50*1024*1024)
		var tooLarge *coreclient.ClipTooLargeError
		if !errors.As(err, &tooLarge) {
			t.Fatalf("expected ClipTooLargeError, got %v", err)
		}
		if tooLarge.MaxAllowedSeconds != 25 {
			t.Fatalf("expected MaxAllowedSeconds 25, got %d", tooLarge.MaxAllowedSeconds)
		}
	})

	t.Run("SetRecorder success", func(t *testing.T) {
		returnStatus = http.StatusOK
		recResp, _ := json.Marshal(contract.RecorderStatus{Enabled: true})
		responseBody = recResp

		st, err := client.SetRecorder(ctx, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !st.Enabled {
			t.Fatal("expected recorder enabled")
		}
	})
}
