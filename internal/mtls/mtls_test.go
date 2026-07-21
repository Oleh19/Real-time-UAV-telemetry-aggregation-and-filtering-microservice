package mtls_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/mtls"
)

type echoServer struct {
	telemetryv1.UnimplementedTelemetryServiceServer
}

func (echoServer) StreamTelemetry(stream telemetryv1.TelemetryService_StreamTelemetryServer) error {
	for {
		if _, err := stream.Recv(); err != nil {
			return stream.SendAndClose(&telemetryv1.StreamSummary{})
		}
	}
}

func generateCerts(t *testing.T) mtls.Files {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("go", "run", "uavmonitor/cmd/certgen", "-dir", dir, "-server-names", "localhost,127.0.0.1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("certgen: %v\n%s", err, out)
	}
	return mtls.Files{
		CACert:     filepath.Join(dir, "ca.crt"),
		ServerCert: filepath.Join(dir, "server.crt"),
		ServerKey:  filepath.Join(dir, "server.key"),
		ClientCert: filepath.Join(dir, "client.crt"),
		ClientKey:  filepath.Join(dir, "client.key"),
	}
}

func startServer(t *testing.T, creds grpc.ServerOption) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	opts := []grpc.ServerOption{}
	if creds != nil {
		opts = append(opts, creds)
	}
	srv := grpc.NewServer(opts...)
	telemetryv1.RegisterTelemetryServiceServer(srv, echoServer{})
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)
	return listener.Addr().String()
}

func dialAndSend(t *testing.T, addr string, opt grpc.DialOption) error {
	t.Helper()
	conn, err := grpc.NewClient(addr, opt)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := telemetryv1.NewTelemetryServiceClient(conn).StreamTelemetry(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&telemetryv1.DroneTelemetry{DroneId: "t1"}); err != nil {
		return err
	}
	_, err = stream.CloseAndRecv()
	return err
}

func TestMutualTLSAcceptsTrustedClient(t *testing.T) {
	files := generateCerts(t)
	serverCreds, err := mtls.ServerCredentials(files)
	if err != nil {
		t.Fatalf("server credentials: %v", err)
	}
	addr := startServer(t, grpc.Creds(serverCreds))

	clientOpt, secure, err := mtls.DialCredentials(files, "localhost")
	if err != nil {
		t.Fatalf("dial credentials: %v", err)
	}
	if !secure {
		t.Fatal("DialCredentials reported insecure with full client files")
	}
	if err := dialAndSend(t, addr, clientOpt); err != nil {
		t.Fatalf("trusted client was rejected: %v", err)
	}
}

func TestMutualTLSRejectsClientWithoutCertificate(t *testing.T) {
	files := generateCerts(t)
	serverCreds, err := mtls.ServerCredentials(files)
	if err != nil {
		t.Fatalf("server credentials: %v", err)
	}
	addr := startServer(t, grpc.Creds(serverCreds))

	if err := dialAndSend(t, addr, grpc.WithTransportCredentials(insecure.NewCredentials())); err == nil {
		t.Fatal("plaintext client was accepted by an mTLS server")
	}
}

func TestDialCredentialsFallsBackToInsecure(t *testing.T) {
	_, secure, err := mtls.DialCredentials(mtls.Files{}, "localhost")
	if err != nil {
		t.Fatalf("DialCredentials: %v", err)
	}
	if secure {
		t.Fatal("empty files should yield insecure credentials")
	}
}

func TestServerCredentialsRequireFiles(t *testing.T) {
	if _, err := mtls.ServerCredentials(mtls.Files{CACert: filepath.Join(t.TempDir(), "missing.crt")}); err == nil {
		t.Fatal("ServerCredentials accepted a missing CA file")
	}
	if !mtls.FilesFromEnv().ServerEnabled() && os.Getenv("TLS_CA_CERT") != "" {
		t.Fatal("FilesFromEnv disagreed with the environment")
	}
}
