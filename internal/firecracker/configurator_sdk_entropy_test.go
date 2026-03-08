package firecracker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPutEntropyDeviceSuccess(t *testing.T) {
	socketPath := newTestUnixSocketPath(t)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if isPermissionDeniedError(err) {
			t.Skipf("unix socket bind permission denied in this environment: %v", err)
		}
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	requestSeen := make(chan struct{}, 1)
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPut {
				t.Errorf("unexpected method: got %s want %s", request.Method, http.MethodPut)
			}
			if request.URL.Path != "/entropy" {
				t.Errorf("unexpected path: got %s want /entropy", request.URL.Path)
			}

			bodyBytes, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Errorf("read body: %v", readErr)
			}
			if body := strings.TrimSpace(string(bodyBytes)); body != "{}" {
				t.Errorf("unexpected body: got %q want {}", body)
			}

			requestSeen <- struct{}{}
			writer.WriteHeader(http.StatusNoContent)
		}),
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		err := <-serverDone
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) && err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := putEntropyDevice(ctx, socketPath); err != nil {
		t.Fatalf("put entropy device: %v", err)
	}

	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatalf("entropy request was not received")
	}
}

func TestPutEntropyDeviceUnexpectedStatus(t *testing.T) {
	socketPath := newTestUnixSocketPath(t)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if isPermissionDeniedError(err) {
			t.Skipf("unix socket bind permission denied in this environment: %v", err)
		}
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"fault_message":"entropy not allowed"}`))
		}),
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
		err := <-serverDone
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) && err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = putEntropyDevice(ctx, socketPath)
	if err == nil {
		t.Fatalf("expected error for non-204 response")
	}
	if !strings.Contains(err.Error(), "unexpected status code 400") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newTestUnixSocketPath(t *testing.T) string {
	t.Helper()

	file, err := os.CreateTemp("", "fc-entropy-*.sock")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	path := file.Name()
	_ = file.Close()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temp file before listen: %v", err)
	}
	return path
}

func isPermissionDeniedError(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "operation not permitted")
}
