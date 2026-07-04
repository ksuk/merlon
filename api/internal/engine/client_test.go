package engine

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func TestWithTLSMissingCertificateFailsClosed(t *testing.T) {
	_, err := NewClient("127.0.0.1:1", WithTLS("does-not-exist.pem", "engine.local"))
	if err == nil {
		t.Fatal("expected missing TLS certificate to fail before dialing")
	}
}

type stubHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	status grpc_health_v1.HealthCheckResponse_ServingStatus
}

func (s *stubHealthServer) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: s.status}, nil
}

func startTestHealthServer(t *testing.T, status grpc_health_v1.HealthCheckResponse_ServingStatus) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, &stubHealthServer{status: status})
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestClient_CheckHealth_Success(t *testing.T) {
	addr := startTestHealthServer(t, grpc_health_v1.HealthCheckResponse_SERVING)

	c, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.CheckHealth(ctx); err != nil {
		t.Errorf("CheckHealth() error = %v, want nil", err)
	}
}

func TestClient_CheckHealth_NotServing(t *testing.T) {
	addr := startTestHealthServer(t, grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	c, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.CheckHealth(ctx); err == nil {
		t.Error("CheckHealth() error = nil, want error for NOT_SERVING status")
	}
}

func TestClient_CheckHealth_EngineDown(t *testing.T) {
	// Nothing listens on this address, simulating the engine being down.
	c, err := NewClient("127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.CheckHealth(ctx); err == nil {
		t.Error("CheckHealth() error = nil, want error for unreachable engine")
	}
}
