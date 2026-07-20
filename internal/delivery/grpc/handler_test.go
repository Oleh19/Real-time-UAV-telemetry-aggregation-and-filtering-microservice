package grpc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	grpcdelivery "uavmonitor/internal/delivery/grpc"
	"uavmonitor/internal/telemetry"
	"uavmonitor/internal/usecase"
)

type fakeStream struct {
	googlegrpc.ServerStream
	ctx      context.Context
	incoming []*telemetryv1.DroneTelemetry
	summary  *telemetryv1.StreamSummary
}

func (s *fakeStream) Context() context.Context {
	return s.ctx
}

func (s *fakeStream) Recv() (*telemetryv1.DroneTelemetry, error) {
	if len(s.incoming) == 0 {
		return nil, io.EOF
	}
	msg := s.incoming[0]
	s.incoming = s.incoming[1:]
	return msg, nil
}

func (s *fakeStream) SendAndClose(summary *telemetryv1.StreamSummary) error {
	s.summary = summary
	return nil
}

type fakeIngestor struct {
	errs      []error
	submitted int
}

func (f *fakeIngestor) Submit(_ context.Context, _ telemetry.Sample) error {
	f.submitted++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return err
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func message(id string) *telemetryv1.DroneTelemetry {
	return &telemetryv1.DroneTelemetry{DroneId: id, Timestamp: timestamppb.Now()}
}

func TestStreamTelemetry(t *testing.T) {
	tests := []struct {
		name         string
		incoming     []*telemetryv1.DroneTelemetry
		submitErrs   []error
		wantCode     codes.Code
		wantReceived int64
		wantDropped  int64
		wantRejected int64
	}{
		{
			name:         "all samples accepted",
			incoming:     []*telemetryv1.DroneTelemetry{message("drone-001"), message("drone-002")},
			wantCode:     codes.OK,
			wantReceived: 2,
		},
		{
			name:         "queue full counts as dropped",
			incoming:     []*telemetryv1.DroneTelemetry{message("drone-001"), message("drone-002")},
			submitErrs:   []error{usecase.ErrQueueFull},
			wantCode:     codes.OK,
			wantReceived: 2,
			wantDropped:  1,
		},
		{
			name:         "invalid sample skipped and counted",
			incoming:     []*telemetryv1.DroneTelemetry{message(""), message("drone-001")},
			submitErrs:   []error{usecase.ErrInvalidSample},
			wantCode:     codes.OK,
			wantReceived: 2,
			wantRejected: 1,
		},
		{
			name:       "shutdown reported as unavailable",
			incoming:   []*telemetryv1.DroneTelemetry{message("drone-001")},
			submitErrs: []error{usecase.ErrShutdown},
			wantCode:   codes.Unavailable,
		},
		{
			name:       "unexpected ingest error reported as unavailable",
			incoming:   []*telemetryv1.DroneTelemetry{message("drone-001")},
			submitErrs: []error{errors.New("boom")},
			wantCode:   codes.Unavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := grpcdelivery.NewHandler(&fakeIngestor{errs: tt.submitErrs}, nil, discardLogger())
			stream := &fakeStream{ctx: context.Background(), incoming: tt.incoming}

			err := handler.StreamTelemetry(stream)

			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("StreamTelemetry code = %v, want %v (err: %v)", got, tt.wantCode, err)
			}
			if tt.wantCode != codes.OK {
				return
			}
			if stream.summary == nil {
				t.Fatal("summary not sent on clean stream close")
			}
			if got := stream.summary.GetReceivedCount(); got != tt.wantReceived {
				t.Errorf("summary.ReceivedCount = %d, want %d", got, tt.wantReceived)
			}
			if got := stream.summary.GetDroppedCount(); got != tt.wantDropped {
				t.Errorf("summary.DroppedCount = %d, want %d", got, tt.wantDropped)
			}
			if got := stream.summary.GetRejectedCount(); got != tt.wantRejected {
				t.Errorf("summary.RejectedCount = %d, want %d", got, tt.wantRejected)
			}
		})
	}
}
