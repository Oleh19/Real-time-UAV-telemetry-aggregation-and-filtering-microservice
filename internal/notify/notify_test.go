package notify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"uavmonitor/internal/telemetry"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sampleBreach() telemetry.ZoneBreach {
	return telemetry.ZoneBreach{
		Zone: telemetry.Zone{ID: 7, Name: "Kyiv Oblast"},
		Sample: telemetry.Sample{
			DroneID:   "drone-001",
			Timestamp: time.Unix(1700000000, 0).UTC(),
			Latitude:  50.45,
			Longitude: 30.52,
			Altitude:  120,
		},
		Event: telemetry.BreachEntered,
	}
}

func TestNotificationText(t *testing.T) {
	text := FromBreach(sampleBreach()).Text()
	if !strings.Contains(text, "drone-001") || !strings.Contains(text, "entered") || !strings.Contains(text, "Kyiv Oblast") {
		t.Fatalf("unexpected notification text: %q", text)
	}
}

func TestWebhookSinkPostsJSON(t *testing.T) {
	var received Notification
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := NewWebhookSink(server.URL, server.Client())
	if err := sink.Send(context.Background(), FromBreach(sampleBreach())); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.DroneID != "drone-001" || received.ZoneID != 7 {
		t.Fatalf("received = %+v, want drone-001 in zone 7", received)
	}
}

func TestWebhookSinkFailsOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sink := NewWebhookSink(server.URL, server.Client())
	if err := sink.Send(context.Background(), FromBreach(sampleBreach())); err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestTelegramSinkSendsMessage(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/botTEST-TOKEN/sendMessage") {
			t.Errorf("path = %q, want telegram sendMessage endpoint", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := NewTelegramSink(server.URL, "TEST-TOKEN", "12345", server.Client())
	if err := sink.Send(context.Background(), FromBreach(sampleBreach())); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if payload["chat_id"] != "12345" || !strings.Contains(payload["text"], "drone-001") {
		t.Fatalf("payload = %+v, want chat 12345 mentioning drone-001", payload)
	}
}

type recordingSink struct {
	name    string
	fail    bool
	calls   int
	lastMsg Notification
}

func (s *recordingSink) Name() string { return s.name }

func (s *recordingSink) Send(_ context.Context, notification Notification) error {
	s.calls++
	s.lastMsg = notification
	if s.fail {
		return context.DeadlineExceeded
	}
	return nil
}

func TestCooldownGateSuppressesRepeatsWithinWindow(t *testing.T) {
	gate := newCooldownGate(time.Minute)
	base := time.Unix(1700000000, 0).UTC()

	if !gate.allow("drone-1|7|entered", base) {
		t.Fatal("first alert should pass the cooldown gate")
	}
	if gate.allow("drone-1|7|entered", base.Add(30*time.Second)) {
		t.Fatal("repeat within the window should be suppressed")
	}
	if !gate.allow("drone-1|7|exited", base.Add(30*time.Second)) {
		t.Fatal("a different event should pass independently")
	}
	if !gate.allow("drone-1|7|entered", base.Add(90*time.Second)) {
		t.Fatal("alert after the window should pass again")
	}
}

func TestCooldownGateDisabledWhenWindowZero(t *testing.T) {
	gate := newCooldownGate(0)
	base := time.Unix(1700000000, 0).UTC()
	for n := range 3 {
		if !gate.allow("k", base) {
			t.Fatalf("a zero window must never suppress (attempt %d)", n)
		}
	}
}

func TestDispatchReportsSuccessWhenAllSinksSucceed(t *testing.T) {
	first := &recordingSink{name: "a"}
	second := &recordingSink{name: "b"}
	d := NewDispatcher([]Sink{first, second}, false, 0, time.Second, discardLogger())

	if !d.dispatch(context.Background(), FromBreach(sampleBreach())) {
		t.Fatal("dispatch reported failure with all sinks succeeding")
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("sink calls = %d, %d, want 1, 1", first.calls, second.calls)
	}
	if d.SentTotal() != 2 || d.FailedTotal() != 0 {
		t.Fatalf("sent=%d failed=%d, want 2 and 0", d.SentTotal(), d.FailedTotal())
	}
}

func TestDispatchReportsFailureWhenAnySinkFails(t *testing.T) {
	ok := &recordingSink{name: "ok"}
	bad := &recordingSink{name: "bad", fail: true}
	d := NewDispatcher([]Sink{ok, bad}, false, 0, time.Second, discardLogger())

	if d.dispatch(context.Background(), FromBreach(sampleBreach())) {
		t.Fatal("dispatch reported success despite a failing sink")
	}
	if d.SentTotal() != 1 || d.FailedTotal() != 1 {
		t.Fatalf("sent=%d failed=%d, want 1 and 1", d.SentTotal(), d.FailedTotal())
	}
}
