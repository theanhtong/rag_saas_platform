package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hibiken/asynq"
)

// TaskProcessor handles background task execution.
type TaskProcessor struct {
	pipeline Pipeline
	queue    IngestQueue
}

// NewTaskProcessor constructs a processor instance.
func NewTaskProcessor(p Pipeline, q IngestQueue) *TaskProcessor {
	return &TaskProcessor{
		pipeline: p,
		queue:    q,
	}
}

// ProcessIngestTask processes document chunking, embedding, and vector DB insertion.
func (processor *TaskProcessor) ProcessIngestTask(ctx context.Context, t *asynq.Task) error {
	var payload IngestTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal task payload: %w", err)
	}

	log.Printf("[WORKER] starting document ingestion task ID=%s filename=%s", payload.TaskID, payload.Filename)

	// update status to processing (50% progress)
	status := &TaskStatus{
		TaskID:     payload.TaskID,
		Status:     StatusProcessing,
		Progress:   50,
		DocumentID: payload.DocumentID,
	}
	_ = processor.queue.UpdateTaskStatus(ctx, status)

	cfg := ChunkingConfig{
		ChunkSize:    payload.ChunkSize,
		ChunkOverlap: payload.ChunkOverlap,
	}

	result, err := processor.pipeline.IngestDocument(ctx, payload.DocumentID, payload.Filename, payload.RawText, cfg)
	if err != nil {
		log.Printf("[WORKER] task ID=%s failed: %v", payload.TaskID, err)
		status.Status = StatusFailed
		status.Error = err.Error()
		status.Progress = 100
		_ = processor.queue.UpdateTaskStatus(ctx, status)
		return err
	}

	log.Printf("[WORKER] task ID=%s completed successfully. inserted %d chunks", payload.TaskID, result.InsertedCount)
	status.Status = StatusCompleted
	status.Progress = 100
	status.DocumentID = result.DocumentID
	_ = processor.queue.UpdateTaskStatus(ctx, status)

	return nil
}
