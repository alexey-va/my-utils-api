CREATE TABLE business_workspaces (
    owner_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    bank_token_ciphertext BYTEA,
    bank_token_nonce BYTEA,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((bank_token_ciphertext IS NULL) = (bank_token_nonce IS NULL))
);

CREATE TABLE business_files (
    owner_id UUID NOT NULL REFERENCES business_workspaces(owner_id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
    data BYTEA NOT NULL CHECK (octet_length(data) > 0 AND octet_length(data) <= 20971520),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, id),
    UNIQUE (owner_id, sha256)
);
