package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theanhtong/rag_system/internal/embedder"
	"github.com/theanhtong/rag_system/internal/ingest"
)

func TestDocumentHandler_HandleIngestSync(t *testing.T) {
	gin.SetMode(gin.TestMode)

	chunker := ingest.NewChunker()
	emb := embedder.NewEmbeddingService(nil)
	pipeline := ingest.NewPipeline(chunker, emb, nil, nil)
	docHandler := NewDocumentHandler(pipeline, nil, nil)

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

func TestDocumentHandler_HandleIngestAsyncAndTaskStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: mr.Addr()})
	defer asynqClient.Close()

	queue := ingest.NewIngestQueue(asynqClient, rdb)
	chunker := ingest.NewChunker()
	emb := embedder.NewEmbeddingService(nil)
	pipeline := ingest.NewPipeline(chunker, emb, nil, rdb)

	docHandler := NewDocumentHandler(pipeline, queue, nil)

	engine := gin.New()
	engine.POST("/v1/documents/ingest", docHandler.HandleIngest)
	engine.GET("/v1/documents/tasks/:id", docHandler.HandleGetTaskStatus)

	reqPayload := IngestTextRequest{
		DocumentID: "doc-async-100",
		Filename:   "async_contract.txt",
		Content:    "Async document processing text body for testing queue behavior.",
	}
	body, err := json.Marshal(reqPayload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/v1/documents/ingest", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Contains(t, w.Body.String(), "queued")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	dataMap, ok := resp["data"].(map[string]interface{})
	require.True(t, ok)
	taskID, ok := dataMap["task_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, taskID)

	// test polling task status endpoint
	reqTask, err := http.NewRequest(http.MethodGet, "/v1/documents/tasks/"+taskID, nil)
	require.NoError(t, err)
	wTask := httptest.NewRecorder()

	engine.ServeHTTP(wTask, reqTask)
	assert.Equal(t, http.StatusOK, wTask.Code)
	assert.Contains(t, wTask.Body.String(), "queued")

	// update task status to completed and poll again
	_ = queue.UpdateTaskStatus(context.Background(), &ingest.TaskStatus{
		TaskID:     taskID,
		Status:     ingest.StatusCompleted,
		Progress:   100,
		DocumentID: "doc-async-100",
	})

	wTask2 := httptest.NewRecorder()
	engine.ServeHTTP(wTask2, reqTask)
	assert.Equal(t, http.StatusOK, wTask2.Code)
	assert.Contains(t, wTask2.Body.String(), "completed")
}
