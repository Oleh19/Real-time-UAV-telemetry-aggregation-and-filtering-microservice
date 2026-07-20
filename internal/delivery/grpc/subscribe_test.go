package grpc_test

import (
	"context"
	"testing"
	"time"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/broadcast"
	grpcdelivery "uavmonitor/internal/delivery/grpc"
	"uavmonitor/internal/telemetry"
)

type fakeSubscribeStream struct {
	googlegrpc.ServerStream
	ctx  context.Context
	sent chan *telemetryv1.DroneTelemetry
}

func (s *fakeSubscribeStream) Context() context.Context {
	return s.ctx
}

func (s *fakeSubscribeStream) Send(msg *telemetryv1.DroneTelemetry) error {
	s.sent <- msg
	return nil
}

func trackSample(id string, confidence int32) telemetry.Sample {
	return telemetry.Sample{
		DroneID:    telemetry.DroneID(id),
		Timestamp:  time.Now(),
		Latitude:   50,
		Longitude:  30,
		Confidence: confidence,
	}
}

func runSubscriber(t *testing.T, hub *broadcast.Hub, req *telemetryv1.SubscribeRequest) (*fakeSubscribeStream, context.CancelFunc, chan error) {
	t.Helper()
	handler := grpcdelivery.NewHandler(&fakeIngestor{}, hub, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeSubscribeStream{ctx: ctx, sent: make(chan *telemetryv1.DroneTelemetry, 16)}
	errCh := make(chan error, 1)
	go func() {
		errCh <- handler.SubscribeTelemetry(req, stream)
	}()
	if !waitFor(func() bool { return hub.Subscribers() == 1 }) {
		t.Fatal("subscriber never registered with the hub")
	}
	return stream, cancel, errCh
}

func waitFor(condition func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return condition()
}

func TestSubscribeTelemetryDeliversMatchingSamples(t *testing.T) {
	hub := broadcast.NewHub(16)
	stream, cancel, errCh := runSubscriber(t, hub, &telemetryv1.SubscribeRequest{
		DroneIds:      []string{"target-001"},
		MinConfidence: 50,
	})

	hub.Broadcast(trackSample("target-001", 90))
	hub.Broadcast(trackSample("target-002", 90))
	hub.Broadcast(trackSample("target-001", 30))
	hub.Broadcast(trackSample("target-001", 60))

	first := <-stream.sent
	second := <-stream.sent
	if first.GetDroneId() != "target-001" || first.GetConfidence() != 90 {
		t.Errorf("first delivery = %s/%d, want target-001/90", first.GetDroneId(), first.GetConfidence())
	}
	if second.GetConfidence() != 60 {
		t.Errorf("second delivery confidence = %d, want 60 (filtered 30 and target-002)", second.GetConfidence())
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("subscriber exit after context cancel = %v, want nil", err)
	}
	if hub.Subscribers() != 0 {
		t.Fatal("subscriber was not removed from the hub")
	}
}

func TestSubscribeTelemetryEndsWithUnavailableOnHubClose(t *testing.T) {
	hub := broadcast.NewHub(16)
	_, cancel, errCh := runSubscriber(t, hub, &telemetryv1.SubscribeRequest{})
	defer cancel()

	hub.Close()
	err := <-errCh
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("exit code = %v, want Unavailable", status.Code(err))
	}
}

func TestSubscribeTelemetryWithoutHubIsUnimplemented(t *testing.T) {
	handler := grpcdelivery.NewHandler(&fakeIngestor{}, nil, discardLogger())
	stream := &fakeSubscribeStream{ctx: context.Background(), sent: make(chan *telemetryv1.DroneTelemetry, 1)}
	err := handler.SubscribeTelemetry(&telemetryv1.SubscribeRequest{}, stream)
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("code = %v, want Unimplemented", status.Code(err))
	}
}
