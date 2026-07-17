package grpc_test

import (
	"context"
	"testing"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	grpcdelivery "uavmonitor/internal/delivery/grpc"
)

type authStream struct {
	googlegrpc.ServerStream
	ctx context.Context
}

func (s authStream) Context() context.Context { return s.ctx }

func TestStreamAuthInterceptor(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		metadata map[string]string
		wantCode codes.Code
	}{
		{
			name:     "auth disabled allows any request",
			token:    "",
			metadata: nil,
			wantCode: codes.OK,
		},
		{
			name:     "valid token passes",
			token:    "secret",
			metadata: map[string]string{"authorization": "Bearer secret"},
			wantCode: codes.OK,
		},
		{
			name:     "missing metadata rejected",
			token:    "secret",
			metadata: nil,
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "missing token rejected",
			token:    "secret",
			metadata: map[string]string{"other": "value"},
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "wrong token rejected",
			token:    "secret",
			metadata: map[string]string{"authorization": "Bearer nope"},
			wantCode: codes.Unauthenticated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := grpcdelivery.StreamAuthInterceptor(tt.token)
			ctx := context.Background()
			if tt.metadata != nil {
				ctx = metadata.NewIncomingContext(ctx, metadata.New(tt.metadata))
			}
			called := false
			handler := func(any, googlegrpc.ServerStream) error {
				called = true
				return nil
			}

			err := interceptor(nil, authStream{ctx: ctx}, &googlegrpc.StreamServerInfo{}, handler)

			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("code = %v, want %v", got, tt.wantCode)
			}
			wantCalled := tt.wantCode == codes.OK
			if called != wantCalled {
				t.Errorf("handler called = %v, want %v", called, wantCalled)
			}
		})
	}
}
