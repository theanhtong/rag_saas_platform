-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Create vector_records table
CREATE TABLE IF NOT EXISTS vector_records (
    id TEXT PRIMARY KEY,
    embedding vector(384),
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create HNSW vector similarity search index
CREATE INDEX IF NOT EXISTS vector_hnsw_idx ON vector_records USING hnsw (embedding vector_cosine_ops);
