-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Create tenants table
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create api_keys table for multi-tenant quota management
CREATE TABLE IF NOT EXISTS api_keys (
    key_hash TEXT PRIMARY KEY,
    tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    monthly_budget_usd NUMERIC(10,4) DEFAULT 10.0000,
    used_budget_usd NUMERIC(10,4) DEFAULT 0.0000,
    token_limit BIGINT DEFAULT 1000000,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create token_usage_logs table
CREATE TABLE IF NOT EXISTS token_usage_logs (
    id SERIAL PRIMARY KEY,
    api_key_hash TEXT REFERENCES api_keys(key_hash) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_tokens INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    cost_usd NUMERIC(10,6) DEFAULT 0.000000,
    timestamp TIMESTAMPTZ DEFAULT NOW()
);

-- Create vector_records table
CREATE TABLE IF NOT EXISTS vector_records (
    id TEXT PRIMARY KEY,
    embedding vector(384),
    metadata JSONB,
    content_tsvector TSVECTOR,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create HNSW vector similarity search index
CREATE INDEX IF NOT EXISTS vector_hnsw_idx ON vector_records USING hnsw (embedding vector_cosine_ops);

-- Create GIN index for full-text search
CREATE INDEX IF NOT EXISTS vector_tsvector_idx ON vector_records USING gin(content_tsvector);

-- Insert default seed tenant and API key for local testing
INSERT INTO tenants (id, name)
VALUES ('tenant_default', 'Default Organization')
ON CONFLICT (id) DO NOTHING;

INSERT INTO api_keys (key_hash, tenant_id, name, monthly_budget_usd, token_limit, is_active)
VALUES ('default_key_hash', 'tenant_default', 'Default Key', 50.0000, 5000000, TRUE)
ON CONFLICT (key_hash) DO NOTHING;

