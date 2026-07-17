package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type SnapshotFunc func(ctx context.Context) any

func Handler(interval time.Duration, snapshot SnapshotFunc, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ctx := r.Context()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		if !writeEvent(w, flusher, snapshot(ctx), logger) {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !writeEvent(w, flusher, snapshot(ctx), logger) {
					return
				}
			}
		}
	}
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, payload any, logger *slog.Logger) bool {
	encoded, err := json.Marshal(payload)
	if err != nil {
		logger.Error("marshal sse event", "error", err)
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
