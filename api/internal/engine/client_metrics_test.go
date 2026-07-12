package engine

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/ksuk/merlon/api/gen/merlon/v1"
	"github.com/ksuk/merlon/api/internal/domain"
	"github.com/ksuk/merlon/api/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
)

// histogramSampleCount reads the current observation count for a labeled
// histogram, since testutil.ToFloat64 only supports Gauge/Counter/Untyped.
func histogramSampleCount(t *testing.T, method, status string) uint64 {
	t.Helper()
	var m dto.Metric
	observer := metrics.GRPCRequestDuration.WithLabelValues(method, status)
	metricIface, ok := observer.(prometheus.Metric)
	if !ok {
		t.Fatalf("observer for method=%s,status=%s does not implement prometheus.Metric", method, status)
	}
	if err := metricIface.Write(&m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return m.GetHistogram().GetSampleCount()
}

type stubScoringServer struct {
	pb.UnimplementedScoringServiceServer
}

func (s *stubScoringServer) EvaluateCustomerRisk(_ context.Context, req *pb.EvaluateCustomerRiskRequest) (*pb.EvaluateCustomerRiskResponse, error) {
	return &pb.EvaluateCustomerRiskResponse{
		CustomerId: req.Customer.CustomerId,
		Score:      1.5,
		Tier:       pb.RiskTier_RISK_TIER_LOW,
	}, nil
}

func startTestScoringServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterScoringServiceServer(srv, &stubScoringServer{})
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestClientRecordsGRPCDuration(t *testing.T) {
	addr := startTestScoringServer(t)

	c, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	before := histogramSampleCount(t, "ScoreCustomer", "ok")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	customer := &domain.Customer{ID: "cust-1", CountryCode: "JP"}
	if _, err := c.ScoreCustomer(ctx, customer, "ruleset-1"); err != nil {
		t.Fatalf("ScoreCustomer: %v", err)
	}

	after := histogramSampleCount(t, "ScoreCustomer", "ok")
	if after <= before {
		t.Errorf("GRPCRequestDuration sample count for method=ScoreCustomer,status=ok did not increase: before=%v after=%v", before, after)
	}
}

func TestClientRecordsGRPCDurationOnError(t *testing.T) {
	// Nothing listens on this address, simulating an RPC failure.
	c, err := NewClient("127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	before := histogramSampleCount(t, "ScoreCustomer", "error")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	customer := &domain.Customer{ID: "cust-1", CountryCode: "JP"}
	if _, err := c.ScoreCustomer(ctx, customer, "ruleset-1"); err == nil {
		t.Fatal("expected ScoreCustomer to fail against an unreachable engine")
	}

	after := histogramSampleCount(t, "ScoreCustomer", "error")
	if after <= before {
		t.Errorf("GRPCRequestDuration sample count for method=ScoreCustomer,status=error did not increase: before=%v after=%v", before, after)
	}
}
