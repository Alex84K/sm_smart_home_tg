package coreclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Alex84K/core_syst_go/contract"
	"github.com/Alex84K/tg_gateway_go/internal/coreclient"
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
}
