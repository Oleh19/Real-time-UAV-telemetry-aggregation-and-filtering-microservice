package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
	"uavmonitor/internal/usecase"
)

type Ingestor interface {
	Submit(ctx context.Context, sample telemetry.Sample) error
}

type Handler struct {
	telemetryv1.UnimplementedTelemetryServiceServer
	ingestor      Ingestor
	subscriptions Subscriptions
	logger        *slog.Logger
}

func NewHandler(ingestor Ingestor, subscriptions Subscriptions, logger *slog.Logger) *Handler {
	return &Handler{ingestor: ingestor, subscriptions: subscriptions, logger: logger}
}

func (h *Handler) StreamTelemetry(stream telemetryv1.TelemetryService_StreamTelemetryServer) error {
	ctx := stream.Context()
	var received, dropped, rejected int64
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&telemetryv1.StreamSummary{
				ReceivedCount: received,
				DroppedCount:  dropped,
				RejectedCount: rejected,
			})
		}
		if err != nil {
			return status.Errorf(codes.Aborted, "receive telemetry: %v", err)
		}
		sample := fromProto(msg)
		received++
		if err := h.ingestor.Submit(ctx, sample); err != nil {
			switch {
			case errors.Is(err, usecase.ErrInvalidSample):
				rejected++
				h.logger.Warn("telemetry rejected", "drone_id", sample.DroneID, "error", err)
			case errors.Is(err, usecase.ErrQueueFull):
				dropped++
				h.logger.Warn("telemetry dropped due to backpressure", "drone_id", sample.DroneID)
			case errors.Is(err, usecase.ErrShutdown):
				return status.Error(codes.Unavailable, "server is shutting down")
			default:
				return status.Errorf(codes.Unavailable, "ingest telemetry: %v", err)
			}
		}
	}
}

func fromProto(msg *telemetryv1.DroneTelemetry) telemetry.Sample {
	return telemetry.Sample{
		DroneID:    telemetry.DroneID(msg.GetDroneId()),
		StationID:  telemetry.StationID(msg.GetStationId()),
		Timestamp:  msg.GetTimestamp().AsTime(),
		Latitude:   msg.GetLatitude(),
		Longitude:  msg.GetLongitude(),
		Altitude:   msg.GetAltitude(),
		Speed:      msg.GetSpeed(),
		Confidence: msg.GetConfidence(),
	}
}
