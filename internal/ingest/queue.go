package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	TypeDocumentIngest = "document:ingest"

	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// IngestTaskPayload contains parameters for async document ingestion.
type IngestTaskPayload struct {
	TaskID       string `json:"task_id"`
	DocumentID   string `json:"document_id"`
	Filename     string `json:"filename"`
	RawText      string `json:"raw_text"`
	ChunkSize    int    `json:"chunk_size"`
	ChunkOverlap int    `json:"chunk_overlap"`
}

// TaskStatus tracks background ingestion task progress and state.
type TaskStatus struct {
	TaskID     string    `json:"task_id"`
	Status     string    `json:"status"`
	Progress   int       `json:"progress"`
	Error      string    `json:"error,omitempty"`
	DocumentID string    `json:"document_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IngestQueue defines the interface for task enqueueing and status polling.
type IngestQueue interface {
	EnqueueIngest(ctx context.Context, payload *IngestTaskPayload) (*TaskStatus, error)
	GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error)
	UpdateTaskStatus(ctx context.Context, status *TaskStatus) error
}

type redisIngestQueue struct {
	client *asynq.Client
	rdb    redis.UniversalClient
}

// NewIngestQueue initializes an Asynq queue client backed by Redis.
func NewIngestQueue(client *asynq.Client, rdb redis.UniversalClient) IngestQueue {
	return &redisIngestQueue{
		client: client,
		rdb:    rdb,
	}
}

func (q *redisIngestQueue) EnqueueIngest(ctx context.Context, payload *IngestTaskPayload) (*TaskStatus, error) {
	if payload.TaskID == "" {
		payload.TaskID = uuid.New().String()
	}
	if payload.DocumentID == "" {
		payload.DocumentID = fmt.Sprintf("doc_%s", uuid.New().String()[:8])
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task payload: %w", err)
	}

	now := time.Now()
	status := &TaskStatus{
		TaskID:     payload.TaskID,
		Status:     StatusQueued,
		Progress:   0,
		DocumentID: payload.DocumentID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := q.UpdateTaskStatus(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to save initial task status: %w", err)
	}

	task := asynq.NewTask(TypeDocumentIngest, bytes, asynq.MaxRetry(3), asynq.Timeout(5*time.Minute))
	info, err := q.client.EnqueueContext(ctx, task)
	if err != nil {
		status.Status = StatusFailed
		status.Error = err.Error()
		_ = q.UpdateTaskStatus(ctx, status)
		return nil, fmt.Errorf("failed to enqueue task: %w", err)
	}

	_ = info
	return status, nil
}

func (q *redisIngestQueue) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error) {
	key := fmt.Sprintf("task_status:%s", taskID)
	val, err := q.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("task status not found for ID: %s", taskID)
	} else if err != nil {
		return nil, fmt.Errorf("redis error fetching task status: %w", err)
	}

	var status TaskStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task status: %w", err)
	}

	return &status, nil
}

func (q *redisIngestQueue) UpdateTaskStatus(ctx context.Context, status *TaskStatus) error {
	status.UpdatedAt = time.Now()
	key := fmt.Sprintf("task_status:%s", status.TaskID)
	bytes, err := json.Marshal(status)
	if err != nil {
		return err
	}

	// store task status with 24-hour expiration
	return q.rdb.Set(ctx, key, bytes, 24*time.Hour).Err()
}
