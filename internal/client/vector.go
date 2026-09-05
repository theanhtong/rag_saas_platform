package client

import (
	"context"
	"fmt"
	"time"

	"github.com/sony/gobreaker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/theanhtong/llm_api_gateway/internal/config"
	pbv1 "github.com/theanhtong/llm_api_gateway/pkg/pb/v1"
)

type VectorClient interface {
	SearchSimilarVectors(ctx context.Context, vector []float32, topK int32) (*pbv1.SearchResponse, error)
	Close() error
}

type vectorClient struct {
	grpcClient pbv1.VectorServiceClient
	conn       *grpc.ClientConn
	cb         *gobreaker.CircuitBreaker
	timeout    time.Duration
}

func NewVectorClient(cfg *config.VectorServiceConfig) (VectorClient, error) {
	conn, err := grpc.Dial(
		cfg.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vector service at %s: %w", cfg.Addr, err)
	}

	cbSettings := gobreaker.Settings{
		Name:        "VectorServiceCircuitBreaker",
		MaxRequests: 5,
		Interval:    10 * time.Second,
		Timeout:     5 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.5
		},
	}

	cb := gobreaker.NewCircuitBreaker(cbSettings)
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &vectorClient{
		grpcClient: pbv1.NewVectorServiceClient(conn),
		conn:       conn,
		cb:         cb,
		timeout:    timeout,
	}, nil
}

func (c *vectorClient) SearchSimilarVectors(ctx context.Context, vector []float32, topK int32) (*pbv1.SearchResponse, error) {
	result, err := c.cb.Execute(func() (interface{}, error) {
		reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		req := &pbv1.SearchRequest{
			Vector: vector,
			TopK:   topK,
		}

		resp, err := c.grpcClient.SearchSimilarVectors(reqCtx, req)
		if err != nil {
			return nil, err
		}
		return resp, nil
	})

	if err != nil {
		return nil, fmt.Errorf("vector service call failed: %w", err)
	}

	return result.(*pbv1.SearchResponse), nil
}

func (c *vectorClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
