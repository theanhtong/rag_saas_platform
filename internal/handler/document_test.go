package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/rag_system/internal/embedder"
	"github.com/theanhtong/rag_system/internal/ingest"
)

func TestDocumentHandler_HandleIngest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	chunker := ingest.NewChunker()
	emb := embedder.NewEmbeddingService(nil)
	pipeline := ingest.NewPipeline(chunker, emb, nil)
	docHandler := NewDocumentHandler(pipeline)

	engine := gin.New()
	engine.POST("/v1/documents/ingest", docHandler.HandleIngest)
	engine.GET("/v1/documents", docHandler.HandleListDocuments)

	reqPayload := IngestTextRequest{
		DocumentID: "doc-test-1",
		Filename:   "policy.txt",
		Content:    "This is sample policy document content for unit testing.",
	}
	body, err := json.Marshal(reqPayload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/v1/documents/ingest", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "doc-test-1")

	// test listing documents
	reqList, err := http.NewRequest(http.MethodGet, "/v1/documents", nil)
	require.NoError(t, err)
	wList := httptest.NewRecorder()

	engine.ServeHTTP(wList, reqList)
	assert.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), "policy.txt")
}
