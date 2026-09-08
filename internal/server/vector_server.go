package server

import (
	"context"
	"time"

	"github.com/theanhtong/rag_system/internal/repository"
	pbv1 "github.com/theanhtong/rag_system/pkg/pb/v1"
)

type VectorServer struct {
	pbv1.UnimplementedVectorServiceServer
	repo repository.VectorRepository
}

// NewVectorServer constructs a gRPC vector service server instance.
func NewVectorServer(repo repository.VectorRepository) *VectorServer {
	return &VectorServer{repo: repo}
}

func (s *VectorServer) SearchSimilarVectors(ctx context.Context, req *pbv1.SearchRequest) (*pbv1.SearchResponse, error) {
	start := time.Now()
	if len(req.GetVector()) == 0 {
		return &pbv1.SearchResponse{Results: nil, ExecutionTimeUs: 0}, nil
	}

	var queryText string
	if req.GetFilter() != nil {
		queryText = req.GetFilter()["query"]
		if queryText == "" {
			queryText = req.GetFilter()["text"]
		}
	}

	records, err := s.repo.SearchHybrid(ctx, req.GetVector(), queryText, req.GetTopK())
	if err != nil {
		return nil, err
	}

	var results []*pbv1.SearchResult
	for _, rec := range records {
		results = append(results, &pbv1.SearchResult{
			Id:       rec.ID,
			Score:    rec.Score,
			Vector:   rec.Vector,
			Metadata: rec.Metadata,
		})
	}

	elapsed := time.Since(start).Microseconds()
	return &pbv1.SearchResponse{
		Results:         results,
		ExecutionTimeUs: elapsed,
	}, nil
}

func (s *VectorServer) InsertBatch(ctx context.Context, req *pbv1.InsertBatchRequest) (*pbv1.InsertBatchResponse, error) {
	pbRecords := req.GetRecords()
	if len(pbRecords) == 0 {
		return &pbv1.InsertBatchResponse{InsertedCount: 0, Success: true}, nil
	}

	var records []*repository.VectorRecord
	for _, rec := range pbRecords {
		records = append(records, &repository.VectorRecord{
			ID:       rec.GetId(),
			Vector:   rec.GetValues(),
			Metadata: rec.GetMetadata(),
		})
	}

	insertedCount, err := s.repo.InsertBatch(ctx, records)
	if err != nil {
		return nil, err
	}

	return &pbv1.InsertBatchResponse{
		InsertedCount: insertedCount,
		Success:       true,
	}, nil
}
