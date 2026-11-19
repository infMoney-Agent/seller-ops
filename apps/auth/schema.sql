CREATE TABLE IF NOT EXISTS users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issuer text NOT NULL,
    subject text NOT NULL,
    email text NOT NULL,
    email_verified boolean NOT NULL,
    name text NOT NULL,
    UNIQUE (issuer, subject)
);
CREATE TABLE IF NOT EXISTS tenants (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL
);
CREATE TABLE IF NOT EXISTS memberships (
    tenant_id bigint NOT NULL REFERENCES tenants,
    user_id bigint NOT NULL REFERENCES users,
    role text NOT NULL CHECK (role IN ('owner', 'member')),
    PRIMARY KEY (tenant_id, user_id)
);
CREATE TABLE IF NOT EXISTS sessions (
    token_hash bytea PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users,
    tenant_id bigint REFERENCES tenants,
    expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS invitations (
    token_hash bytea PRIMARY KEY,
    tenant_id bigint NOT NULL REFERENCES tenants,
    email text NOT NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at timestamptz
);
CREATE TABLE IF NOT EXISTS products (
    tenant_id bigint NOT NULL REFERENCES tenants,
    id bigint GENERATED ALWAYS AS IDENTITY,
    name text NOT NULL,
    category text NOT NULL,
    price numeric(14,2) NOT NULL CHECK (price >= 0),
    description text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
