package client

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/theanhtong/rag_system/internal/config"
	pbv1 "github.com/theanhtong/rag_system/pkg/pb/v1"
)

type mockVectorServer struct {
	pbv1.UnimplementedVectorServiceServer
}

func (m *mockVectorServer) SearchSimilarVectors(ctx context.Context, req *pbv1.SearchRequest) (*pbv1.SearchResponse, error) {
	return &pbv1.SearchResponse{
		Results: []*pbv1.SearchResult{
			{
				Id:     "vec-1",
				Score:  0.92,
				Vector: req.Vector,
				Metadata: map[string]string{
					"prompt": "Hello world",
				},
			},
		},
		ExecutionTimeUs: 150,
	}, nil
}

func (m *mockVectorServer) InsertBatch(ctx context.Context, req *pbv1.InsertBatchRequest) (*pbv1.InsertBatchResponse, error) {
	return &pbv1.InsertBatchResponse{
		InsertedCount: int32(len(req.Records)),
		Success:       true,
	}, nil
}

func TestVectorClient_SearchSimilarVectors(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	pbv1.RegisterVectorServiceServer(grpcServer, &mockVectorServer{})

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	cfg := &config.VectorServiceConfig{
		Addr:      lis.Addr().String(),
		TimeoutMS: 2000,
	}

	client, err := NewVectorClient(cfg)
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.SearchSimilarVectors(context.Background(), []float32{0.1, 0.2, 0.3}, 1)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Results, 1)
	assert.Equal(t, "vec-1", resp.Results[0].Id)
	assert.Equal(t, float32(0.92), resp.Results[0].Score)

	records := []*pbv1.VectorRecord{
		{
			Id:     "doc-1",
			Values: []float32{0.1, 0.2, 0.3},
			Metadata: map[string]string{
				"text": "sample text",
			},
		},
	}
	insertResp, err := client.InsertBatch(context.Background(), records)
	require.NoError(t, err)
	assert.True(t, insertResp.Success)
	assert.Equal(t, int32(1), insertResp.InsertedCount)
}
