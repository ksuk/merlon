package engine

import (
	"context"
	"fmt"
	"time"

	pb "github.com/merlon-aml/merlon/api/gen/merlon/v1"
	"github.com/merlon-aml/merlon/api/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	scoring    pb.ScoringServiceClient
	monitoring pb.MonitoringServiceClient
	conn       *grpc.ClientConn
}

func NewClient(addr string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("engine dial: %w", err)
	}

	return &Client{
		scoring:    pb.NewScoringServiceClient(conn),
		monitoring: pb.NewMonitoringServiceClient(conn),
		conn:       conn,
	}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func customerTypeToProto(ct domain.CustomerType) pb.CustomerType {
	switch ct {
	case domain.CustomerTypeIndividual:
		return pb.CustomerType_CUSTOMER_TYPE_INDIVIDUAL
	case domain.CustomerTypeCorporateDomestic:
		return pb.CustomerType_CUSTOMER_TYPE_CORPORATE_DOMESTIC
	case domain.CustomerTypeCorporateForeign:
		return pb.CustomerType_CUSTOMER_TYPE_CORPORATE_FOREIGN
	default:
		return pb.CustomerType_CUSTOMER_TYPE_UNSPECIFIED
	}
}

func riskTierFromProto(t pb.RiskTier) domain.RiskTier {
	switch t {
	case pb.RiskTier_RISK_TIER_LOW:
		return domain.RiskTierLow
	case pb.RiskTier_RISK_TIER_MEDIUM:
		return domain.RiskTierMedium
	case pb.RiskTier_RISK_TIER_HIGH:
		return domain.RiskTierHigh
	default:
		return domain.RiskTierMedium
	}
}

func riskTierToProto(t domain.RiskTier) pb.RiskTier {
	switch t {
	case domain.RiskTierLow:
		return pb.RiskTier_RISK_TIER_LOW
	case domain.RiskTierMedium:
		return pb.RiskTier_RISK_TIER_MEDIUM
	case domain.RiskTierHigh:
		return pb.RiskTier_RISK_TIER_HIGH
	default:
		return pb.RiskTier_RISK_TIER_MEDIUM
	}
}

func directionToProto(d domain.TransactionDirection) pb.TransactionDirection {
	switch d {
	case domain.DirectionInbound:
		return pb.TransactionDirection_TRANSACTION_DIRECTION_INBOUND
	case domain.DirectionOutbound:
		return pb.TransactionDirection_TRANSACTION_DIRECTION_OUTBOUND
	case domain.DirectionInternal:
		return pb.TransactionDirection_TRANSACTION_DIRECTION_INTERNAL
	default:
		return pb.TransactionDirection_TRANSACTION_DIRECTION_UNSPECIFIED
	}
}

func alertSeverityFromProto(s pb.AlertSeverity) domain.AlertSeverity {
	switch s {
	case pb.AlertSeverity_ALERT_SEVERITY_LOW:
		return domain.AlertSeverityLow
	case pb.AlertSeverity_ALERT_SEVERITY_MEDIUM:
		return domain.AlertSeverityMedium
	case pb.AlertSeverity_ALERT_SEVERITY_HIGH:
		return domain.AlertSeverityHigh
	case pb.AlertSeverity_ALERT_SEVERITY_CRITICAL:
		return domain.AlertSeverityCritical
	default:
		return domain.AlertSeverityMedium
	}
}

func (c *Client) ScoreCustomer(ctx context.Context, customer *domain.Customer, ruleSetID string) (*domain.ScoreRecord, error) {
	resp, err := c.scoring.EvaluateCustomerRisk(ctx, &pb.EvaluateCustomerRiskRequest{
		Customer: &pb.CustomerAttributes{
			CustomerId:   customer.ID,
			CustomerType: customerTypeToProto(customer.CustomerType),
			CountryCode:  customer.CountryCode,
			ProductTypes: customer.ProductTypes,
		},
		RuleSetId: ruleSetID,
	})
	if err != nil {
		return nil, fmt.Errorf("scoring rpc: %w", err)
	}

	tier := riskTierFromProto(resp.Tier)

	var factors []domain.Factor
	for _, f := range resp.Factors {
		factors = append(factors, domain.Factor{
			Name:        f.Name,
			Axis:        f.Axis,
			Score:       f.Score,
			Description: f.Description,
		})
	}

	now := time.Now()
	return &domain.ScoreRecord{
		CustomerID:     resp.CustomerId,
		Score:          resp.Score,
		Tier:           tier,
		Factors:        factors,
		RuleSetID:      resp.RuleSetId,
		RuleSetVersion: int(resp.RuleSetVersion),
		ScoredAt:       now,
	}, nil
}

func (c *Client) EvaluateTransactions(
	ctx context.Context,
	customerID string,
	riskTier domain.RiskTier,
	transactions []domain.Transaction,
	scenarioIDs []string,
) ([]domain.Alert, error) {
	var pbTxns []*pb.TransactionData
	for _, t := range transactions {
		pbTxns = append(pbTxns, &pb.TransactionData{
			TransactionId:      t.ID,
			CustomerId:         t.CustomerID,
			Amount:             t.Amount,
			Currency:           t.Currency,
			CounterpartyId:     t.CounterpartyID,
			CounterpartyCountry: t.CounterpartyCountry,
			Direction:          directionToProto(t.Direction),
			ExecutedAt: &pb.Timestamp{
				Seconds: t.ExecutedAt.Unix(),
				Nanos:   int32(t.ExecutedAt.Nanosecond()),
			},
			Channel: t.Channel,
		})
	}

	resp, err := c.monitoring.EvaluateTransactions(ctx, &pb.EvaluateTransactionsRequest{
		CustomerId:       customerID,
		CustomerRiskTier: riskTierToProto(riskTier),
		Transactions:     pbTxns,
		ScenarioIds:      scenarioIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("monitoring rpc: %w", err)
	}

	var alerts []domain.Alert
	now := time.Now()
	for _, a := range resp.Alerts {
		alerts = append(alerts, domain.Alert{
			ID:             a.AlertId,
			CustomerID:     a.CustomerId,
			ScenarioID:     a.ScenarioId,
			Severity:       alertSeverityFromProto(a.Severity),
			Status:         domain.AlertStatusOpen,
			Score:          a.Score,
			Description:    a.Description,
			TransactionIDs: a.TransactionIds,
			DetectedAt:     now,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}

	return alerts, nil
}
