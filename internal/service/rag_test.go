package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbv1 "github.com/theanhtong/rag_system/pkg/pb/v1"
)

type mockVectorClient struct {
	results []*pbv1.SearchResult
	err     error
}

func (m *mockVectorClient) SearchSimilarVectors(ctx context.Context, vector []float32, topK int32) (*pbv1.SearchResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &pbv1.SearchResponse{
		Results:         m.results,
		ExecutionTimeUs: 100,
	}, nil
}

func (m *mockVectorClient) InsertBatch(ctx context.Context, records []*pbv1.VectorRecord) (*pbv1.InsertBatchResponse, error) {
	return &pbv1.InsertBatchResponse{InsertedCount: int32(len(records)), Success: true}, nil
}

func (m *mockVectorClient) Close() error {
	return nil
}

func TestRAGService_RetrieveContext(t *testing.T) {
	mockResults := []*pbv1.SearchResult{
		{
			Id:    "chunk_1",
			Score: 0.92,
			Metadata: map[string]string{
				"filename": "policy2026.txt",
				"content":  "Employees working remotely must enable MFA.",
			},
		},
		{
			Id:    "chunk_2",
			Score: 0.85,
			Metadata: map[string]string{
				"filename": "security_guide.txt",
				"content":  "VPN access requires role-based authentication.",
			},
		},
	}

	client := &mockVectorClient{results: mockResults}
	svc := NewRAGService(client)

	promptVector := make([]float32, 384)
	promptVector[0] = 1.0

	ctx := context.Background()
	res, err := svc.RetrieveContext(ctx, "What are the VPN policy rules?", promptVector, 3)
	require.NoError(t, err)
	assert.True(t, res.HasContext)
	assert.Len(t, res.Citations, 2)
	assert.Contains(t, res.Citations[0], "policy2026.txt")
	assert.Contains(t, res.AugmentedPrompt, "Employees working remotely must enable MFA.")
	assert.Contains(t, res.AugmentedPrompt, "User Question: What are the VPN policy rules?")
}

func TestRAGService_RetrieveContext_NoResults(t *testing.T) {
	client := &mockVectorClient{results: nil}
	svc := NewRAGService(client)

	promptVector := make([]float32, 384)

	res, err := svc.RetrieveContext(context.Background(), "General question", promptVector, 3)
	require.NoError(t, err)
	assert.False(t, res.HasContext)
	assert.Equal(t, "General question", res.AugmentedPrompt)
	assert.Nil(t, res.Citations)
}
