package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/theanhtong/rag_system/internal/client"
)

// RAGContextResult represents the retrieved document context for prompt augmentation.
type RAGContextResult struct {
	AugmentedPrompt string   `json:"augmented_prompt"`
	Citations       []string `json:"citations"`
	HasContext      bool     `json:"has_context"`
}

// RAGService defines operations for RAG document context retrieval.
type RAGService interface {
	RetrieveContext(ctx context.Context, prompt string, promptVector []float32, topK int32) (*RAGContextResult, error)
}

type ragService struct {
	vectorClient client.VectorClient
}

// NewRAGService constructs a RAGService instance.
func NewRAGService(vc client.VectorClient) RAGService {
	return &ragService{vectorClient: vc}
}

func (s *ragService) RetrieveContext(ctx context.Context, prompt string, promptVector []float32, topK int32) (*RAGContextResult, error) {
	if s.vectorClient == nil || len(promptVector) == 0 {
		return &RAGContextResult{
			AugmentedPrompt: prompt,
			Citations:       nil,
			HasContext:      false,
		}, nil
	}

	if topK <= 0 {
		topK = 3
	}

	// execute hybrid search passing prompt in query filter for combined cosine + tsvector ranking
	searchResp, err := s.vectorClient.SearchSimilarVectors(ctx, promptVector, topK)
	if err != nil || searchResp == nil || len(searchResp.Results) == 0 {
		return &RAGContextResult{
			AugmentedPrompt: prompt,
			Citations:       nil,
			HasContext:      false,
		}, nil
	}

	var contextBlocks []string
	var citations []string

	for i, res := range searchResp.Results {
		content := strings.TrimSpace(res.Metadata["content"])
		if content == "" {
			content = strings.TrimSpace(res.Metadata["text"])
		}

		if content != "" {
			filename := res.Metadata["filename"]
			docID := res.Metadata["document_id"]
			if filename == "" {
				filename = docID
			}
			if filename == "" {
				filename = "doc"
			}

			contextBlocks = append(contextBlocks, fmt.Sprintf("[%s]: %s", filename, content))
			citations = append(citations, fmt.Sprintf("[%d] %s", i+1, filename))
		}
	}

	if len(contextBlocks) == 0 {
		return &RAGContextResult{
			AugmentedPrompt: prompt,
			Citations:       nil,
			HasContext:      false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("Below is relevant context retrieved from enterprise documents:\n\n")
	for _, block := range contextBlocks {
		sb.WriteString(block)
		sb.WriteString("\n\n")
	}
	sb.WriteString("User Question: ")
	sb.WriteString(prompt)

	return &RAGContextResult{
		AugmentedPrompt: sb.String(),
		Citations:       citations,
		HasContext:      true,
	}, nil
}
