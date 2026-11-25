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

-- A signed-in account belongs to at most one company.
CREATE UNIQUE INDEX IF NOT EXISTS memberships_one_company_per_user ON memberships(user_id);

-- Stable identity organization mappings; display names are never authorization keys.
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS casdoor_org text;
CREATE UNIQUE INDEX IF NOT EXISTS tenants_casdoor_org ON tenants(casdoor_org) WHERE casdoor_org IS NOT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS casdoor_org text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS auth_login_flows (
    token_hash bytea PRIMARY KEY,
    payload jsonb NOT NULL,
    submitting boolean NOT NULL DEFAULT false,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS auth_login_flows_expiry ON auth_login_flows(expires_at);

CREATE TABLE IF NOT EXISTS registration_flows (
 token_hash bytea PRIMARY KEY, csrf text NOT NULL,
 mode text NOT NULL DEFAULT 'create' CHECK(mode IN ('create','join','invite')),
 invitation_hash bytea, target_tenant bigint REFERENCES tenants,
 payload jsonb NOT NULL DEFAULT '{}', result jsonb,
 state text NOT NULL DEFAULT 'editing' CHECK(state IN ('editing','working','done','cleanup_required')),
 error text NOT NULL DEFAULT '', expires_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS registration_flows_expiry ON registration_flows(expires_at);
CREATE TABLE IF NOT EXISTS company_join_requests (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 tenant_id bigint NOT NULL REFERENCES tenants,
 user_id bigint NOT NULL UNIQUE REFERENCES users,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','approved','rejected')),
 note text NOT NULL DEFAULT '', decided_by bigint REFERENCES users,
 created_at timestamptz NOT NULL DEFAULT now(), decided_at timestamptz
);
CREATE INDEX IF NOT EXISTS company_pending_requests ON company_join_requests(tenant_id,state,created_at);
