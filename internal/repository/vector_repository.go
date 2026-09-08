package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// VectorRecord represents an individual vector entry with metadata.
type VectorRecord struct {
	ID       string            `json:"id"`
	Vector   []float32         `json:"vector"`
	Score    float32           `json:"score"`
	Metadata map[string]string `json:"metadata"`
}

// VectorRepository defines database operations for vector storage and search.
type VectorRepository interface {
	InsertBatch(ctx context.Context, records []*VectorRecord) (int32, error)
	SearchHybrid(ctx context.Context, vector []float32, filterText string, topK int32) ([]*VectorRecord, error)
}

type pgVectorRepository struct {
	db *sql.DB
}

// NewVectorRepository constructs a new VectorRepository instance.
func NewVectorRepository(db *sql.DB) VectorRepository {
	return &pgVectorRepository{db: db}
}

func (r *pgVectorRepository) InsertBatch(ctx context.Context, records []*VectorRecord) (int32, error) {
	if len(records) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vector_records (id, embedding, metadata, content_tsvector)
		VALUES ($1, $2, $3, to_tsvector('english', COALESCE($4, '')))
		ON CONFLICT (id) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			metadata = EXCLUDED.metadata,
			content_tsvector = EXCLUDED.content_tsvector,
			created_at = NOW()
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	var insertedCount int32
	for _, rec := range records {
		vectorStr := float32SliceToString(rec.Vector)
		metadataJSON, _ := json.Marshal(rec.Metadata)

		textChunk := rec.Metadata["content"]
		if textChunk == "" {
			textChunk = rec.Metadata["text"]
		}

		_, err := stmt.ExecContext(ctx, rec.ID, vectorStr, metadataJSON, textChunk)
		if err != nil {
			log.Printf("[REPO] failed to insert record %s: %v", rec.ID, err)
			continue
		}
		insertedCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return insertedCount, nil
}

func (r *pgVectorRepository) SearchHybrid(ctx context.Context, vector []float32, filterText string, topK int32) ([]*VectorRecord, error) {
	if len(vector) == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = 5
	}

	vectorStr := float32SliceToString(vector)

	query := `
		SELECT id,
		       (0.7 * (1 - (embedding <=> $1)) + 0.3 * COALESCE(ts_rank(content_tsvector, plainto_tsquery('english', $2)), 0)) AS score,
		       metadata
		FROM vector_records
		ORDER BY score DESC
		LIMIT $3
	`

	rows, err := r.db.QueryContext(ctx, query, vectorStr, filterText, topK)
	if err != nil {
		// fallback to pure vector similarity if tsvector query fails
		fallbackQuery := `
			SELECT id, 1 - (embedding <=> $1) AS score, metadata
			FROM vector_records
			ORDER BY embedding <=> $1
			LIMIT $2
		`
		rows, err = r.db.QueryContext(ctx, fallbackQuery, vectorStr, topK)
		if err != nil {
			return nil, fmt.Errorf("vector search query failed: %w", err)
		}
	}
	defer rows.Close()

	var results []*VectorRecord
	for rows.Next() {
		var id string
		var score float32
		var metadataJSON []byte

		if err := rows.Scan(&id, &score, &metadataJSON); err != nil {
			log.Printf("[REPO] row scan error: %v", err)
			continue
		}

		metadata := make(map[string]string)
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &metadata)
		}

		results = append(results, &VectorRecord{
			ID:       id,
			Vector:   vector,
			Score:    score,
			Metadata: metadata,
		})
	}

	return results, nil
}

func float32SliceToString(slice []float32) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range slice {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'f', 6, 32))
	}
	sb.WriteString("]")
	return sb.String()
}
