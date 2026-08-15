package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hxz0727/API-Switch/internal/monitor"
)

// startSSEServer starts a real HTTP server on 127.0.0.1:0 that serves
// /admin/events with the given handler, and returns its port.
func startSSEServer(t *testing.T, handler http.HandlerFunc) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return port
}

func TestMonitorConnect_Success(t *testing.T) {
	port := startSSEServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)

		ev := monitor.RequestEvent{
			ID:        "req_monitor",
			Timestamp: time.Now(),
			Model:     "m",
			Provider:  "p",
			Status:    "ok",
			Duration:  7 * time.Millisecond,
		}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
		// Returning closes the body, ending the stream so MonitorConnect
		// exits its scan loop.
	})

	err := MonitorConnect(fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("MonitorConnect returned error: %v", err)
	}
}

func TestMonitorConnect_ServerError(t *testing.T) {
	port := startSSEServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	err := MonitorConnect(fmt.Sprintf(":%d", port))
	if err == nil {
		t.Fatal("expected error when server returns non-200")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention HTTP 500, got %q", err)
	}
}
