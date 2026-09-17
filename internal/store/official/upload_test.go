package official

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadLPKCancellationReturnsPromptly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.lpk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 16<<20)
	var state uint32 = 1
	for index := range data {
		state = state*1664525 + 1013904223
		data[index] = byte(state >> 24)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	uploadStarted := make(chan struct{}, 1)
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case uploadStarted <- struct{}{}:
		default:
		}
		select {
		case <-request.Context().Done():
		case <-releaseHandler:
		}
	}))
	defer func() {
		close(releaseHandler)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := uploadLPK(ctx, server.Client(), server.URL, "ci-token", path, "application.lpk")
		done <- err
	}()

	select {
	case <-uploadStarted:
	case err := <-done:
		t.Fatalf("official upload returned before starting: %v", err)
	case <-time.After(time.Second):
		t.Fatal("official upload did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("official upload did not stop after cancellation")
	}
}
