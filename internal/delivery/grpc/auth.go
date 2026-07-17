package grpc

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	authMetadataKey = "authorization"
	bearerPrefix    = "Bearer "
)

func StreamAuthInterceptor(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if token != "" {
			if err := authorize(ss.Context(), token); err != nil {
				return err
			}
		}
		return handler(srv, ss)
	}
}

func authorize(ctx context.Context, token string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get(authMetadataKey)
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization token")
	}
	provided := strings.TrimPrefix(values[0], bearerPrefix)
	if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid authorization token")
	}
	return nil
}
