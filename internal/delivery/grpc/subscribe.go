package grpc

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type Subscriptions interface {
	Subscribe() (int64, <-chan telemetry.Sample)
	Unsubscribe(id int64)
}

func (h *Handler) SubscribeTelemetry(req *telemetryv1.SubscribeRequest, stream telemetryv1.TelemetryService_SubscribeTelemetryServer) error {
	if h.subscriptions == nil {
		return status.Error(codes.Unimplemented, "subscriptions are not enabled")
	}
	id, samples := h.subscriptions.Subscribe()
	defer h.subscriptions.Unsubscribe(id)

	filter := newSubscriptionFilter(req)
	h.logger.Info("subscriber connected", "subscription_id", id)
	defer h.logger.Info("subscriber disconnected", "subscription_id", id)

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case sample, open := <-samples:
			if !open {
				return status.Error(codes.Unavailable, "server is shutting down")
			}
			if !filter.matches(sample) {
				continue
			}
			if err := stream.Send(sampleToProto(sample)); err != nil {
				return status.Errorf(codes.Aborted, "send telemetry to subscriber: %v", err)
			}
		}
	}
}

type subscriptionFilter struct {
	droneIDs      map[telemetry.DroneID]struct{}
	minConfidence int32
}

func newSubscriptionFilter(req *telemetryv1.SubscribeRequest) subscriptionFilter {
	filter := subscriptionFilter{minConfidence: req.GetMinConfidence()}
	if ids := req.GetDroneIds(); len(ids) > 0 {
		filter.droneIDs = make(map[telemetry.DroneID]struct{}, len(ids))
		for _, id := range ids {
			filter.droneIDs[telemetry.DroneID(id)] = struct{}{}
		}
	}
	return filter
}

func (f subscriptionFilter) matches(sample telemetry.Sample) bool {
	if sample.Confidence < f.minConfidence {
		return false
	}
	if f.droneIDs == nil {
		return true
	}
	_, ok := f.droneIDs[sample.DroneID]
	return ok
}

func sampleToProto(sample telemetry.Sample) *telemetryv1.DroneTelemetry {
	return &telemetryv1.DroneTelemetry{
		DroneId:        string(sample.DroneID),
		StationId:      string(sample.StationID),
		Classification: string(sample.Class),
		Timestamp:      timestamppb.New(sample.Timestamp),
		Latitude:       sample.Latitude,
		Longitude:      sample.Longitude,
		Altitude:       sample.Altitude,
		Speed:          sample.Speed,
		Confidence:     sample.Confidence,
	}
}
