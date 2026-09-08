package ingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngestQueue_EnqueueAndGetStatus(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: mr.Addr()})
	defer asynqClient.Close()

	q := NewIngestQueue(asynqClient, rdb)

	ctx := context.Background()
	payload := &IngestTaskPayload{
		TaskID:       "task-test-123",
		DocumentID:   "doc-test-123",
		Filename:     "manual.txt",
		RawText:      "sample document content for unit test",
		ChunkSize:    300,
		ChunkOverlap: 30,
	}

	status, err := q.EnqueueIngest(ctx, payload)
	require.NoError(t, err)
	assert.Equal(t, "task-test-123", status.TaskID)
	assert.Equal(t, StatusQueued, status.Status)
	assert.Equal(t, 0, status.Progress)

	// test fetching task status
	fetchedStatus, err := q.GetTaskStatus(ctx, "task-test-123")
	require.NoError(t, err)
	assert.Equal(t, "task-test-123", fetchedStatus.TaskID)
	assert.Equal(t, StatusQueued, fetchedStatus.Status)

	// test updating task status
	fetchedStatus.Status = StatusCompleted
	fetchedStatus.Progress = 100
	err = q.UpdateTaskStatus(ctx, fetchedStatus)
	require.NoError(t, err)

	updatedStatus, err := q.GetTaskStatus(ctx, "task-test-123")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, updatedStatus.Status)
	assert.Equal(t, 100, updatedStatus.Progress)
}

func TestTaskProcessor_ProcessIngestTask(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: mr.Addr()})
	defer asynqClient.Close()

	q := NewIngestQueue(asynqClient, rdb)
	chunker := NewChunker()
	pipeline := NewPipeline(chunker, nil, nil, rdb)
	processor := NewTaskProcessor(pipeline, q)

	payload := &IngestTaskPayload{
		TaskID:       "task-proc-456",
		DocumentID:   "doc-proc-456",
		Filename:     "report.txt",
		RawText:      "Enterprise report context for worker test processing.",
		ChunkSize:    200,
		ChunkOverlap: 20,
	}

	status, err := q.EnqueueIngest(context.Background(), payload)
	require.NoError(t, err)
	assert.Equal(t, StatusQueued, status.Status)

	// simulate task execution
	taskBytes, err := miniredisTaskPayload(payload)
	require.NoError(t, err)
	asynqTask := asynq.NewTask(TypeDocumentIngest, taskBytes)

	err = processor.ProcessIngestTask(context.Background(), asynqTask)
	require.NoError(t, err)

	finalStatus, err := q.GetTaskStatus(context.Background(), "task-proc-456")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, finalStatus.Status)
	assert.Equal(t, 100, finalStatus.Progress)
}

func miniredisTaskPayload(payload *IngestTaskPayload) ([]byte, error) {
	return json.Marshal(payload)
}

var _ = time.Millisecond
