package coreclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
}

func TestCoreClientClips(t *testing.T) {
	var gotPath, gotMethod string
	var returnStatus int
	var responseBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if returnStatus == http.StatusOK {
			w.Header().Set("Content-Type", "video/mp4")
		} else {
			w.Header().Set("Content-Type", "application/json")
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
	errBody, _ := json.Marshal(contract.ErrorResponse{Error: "x"})

	t.Run("StartClip success", func(t *testing.T) {
		returnStatus = http.StatusCreated
		responseBody, _ = json.Marshal(contract.ClipRecording{Id: "abc", MaxSeconds: 60})

		rec, err := client.StartClip(ctx)
		if err != nil {
			t.Fatalf("StartClip: %v", err)
		}
		if gotMethod != http.MethodPost || gotPath != "/api/clips" {
			t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
		}
		if rec.ID != "abc" || rec.MaxDuration != 60*time.Second {
			t.Fatalf("unexpected recording: %+v", rec)
		}
	})

	startErrors := []struct {
		status int
		want   error
	}{
		{http.StatusConflict, coreclient.ErrClipBusy},
		{http.StatusServiceUnavailable, coreclient.ErrCameraOffline},
		{http.StatusBadGateway, coreclient.ErrCoreUnavailable},
	}
	for _, tt := range startErrors {
		t.Run(fmt.Sprintf("StartClip %d", tt.status), func(t *testing.T) {
			returnStatus, responseBody = tt.status, errBody
			if _, err := client.StartClip(ctx); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("StopClip success", func(t *testing.T) {
		returnStatus, responseBody = http.StatusOK, []byte("mp4-data")

		data, err := client.StopClip(ctx, "abc")
		if err != nil {
			t.Fatalf("StopClip: %v", err)
		}
		if gotMethod != http.MethodPost || gotPath != "/api/clips/abc/stop" {
			t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
		}
		if string(data) != "mp4-data" {
			t.Fatalf("unexpected data %q", data)
		}
	})

	stopErrors := []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, coreclient.ErrClipNotFound},
		{http.StatusUnprocessableEntity, coreclient.ErrClipEmpty},
		{http.StatusInternalServerError, coreclient.ErrClipFailed},
	}
	for _, tt := range stopErrors {
		t.Run(fmt.Sprintf("StopClip %d", tt.status), func(t *testing.T) {
			returnStatus, responseBody = tt.status, errBody
			if _, err := client.StopClip(ctx, "abc"); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}
