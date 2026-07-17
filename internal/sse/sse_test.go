package sse_test

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"uavmonitor/internal/sse"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func readDataLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse line: %v", err)
		}
		if strings.HasPrefix(line, "data:") {
			return strings.TrimSpace(line)
		}
	}
}

func openStream(t *testing.T, handler http.HandlerFunc) (*bufio.Reader, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	cleanup := func() {
		cancel()
		resp.Body.Close()
		server.Close()
	}
	return bufio.NewReader(resp.Body), cleanup
}

func TestHandlerWritesInitialSnapshotImmediately(t *testing.T) {
	handler := sse.Handler(time.Hour, func(context.Context) any {
		return map[string]int{"value": 42}
	}, discardLogger())

	reader, cleanup := openStream(t, handler)
	defer cleanup()

	if line := readDataLine(t, reader); line != `data: {"value":42}` {
		t.Errorf("first event = %q, want the initial snapshot", line)
	}
}

func TestHandlerTicksRepeatedly(t *testing.T) {
	var counter atomic.Int64
	handler := sse.Handler(15*time.Millisecond, func(context.Context) any {
		return counter.Add(1)
	}, discardLogger())

	reader, cleanup := openStream(t, handler)
	defer cleanup()

	first := readDataLine(t, reader)
	second := readDataLine(t, reader)
	if first == second {
		t.Errorf("expected ticker to emit changing snapshots, got %q twice", first)
	}
}
