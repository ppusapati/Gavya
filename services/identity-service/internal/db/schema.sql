-- Who is calling, and what they are allowed to be.
--
-- Everything else in this platform is scoped by tenant, and until now the tenant
-- was whatever the caller wrote in the request body. That closes the case where
-- a query forgets its tenant — which has happened twice here — and does nothing
-- about a caller that asks for another tenant on purpose. This is the table set
-- that lets the tenant come from something that was checked.
--
--
-- A PERSON IS NOT OWNED BY A TENANT
--
-- The obvious shape is a tenant_id on the user, and it is wrong for this
-- business. A federation manager oversees a dozen societies; a veterinarian
-- works across four; a plant operator moves between two chilling centres. Giving
-- each of them one login per tenant means one password per tenant, one password
-- reset per tenant, and — the part that matters — no way to answer "who is this
-- person" across the estate when something goes wrong.
--
-- So identity is global and membership is per-tenant. The cost is that `users`
-- cannot be isolated by a tenant column, since it has none. It is isolated by
-- membership instead: a tenant sees a user exactly when that user is one of
-- theirs. That is a policy this schema can express and the database can enforce.
--
--
-- WHY SESSIONS ARE ROWS
--
-- A signed token that is checked only by its signature cannot be withdrawn. The
-- gap between "this person has left" and "this person's token expires" is
-- whatever the lifetime is, and during it the token still works — which makes
-- dismissal, credential theft and a lost laptop all unhandleable.
--
-- So a token names a session and the session is a row. Checking it costs one
-- indexed lookup per request and buys revocation that takes effect on the next
-- request rather than at expiry.

-- ---------------------------------------------------------------------------
-- People
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(26) PRIMARY KEY,

    -- Stored case-folded, and unique on that basis. Addresses differ only in
    -- case far more often than anyone intends, and two accounts for one person
    -- is how a revoked login stays usable.
    email VARCHAR(320) NOT NULL,
    email_normalised VARCHAR(320) NOT NULL UNIQUE,

    full_name VARCHAR(200) NOT NULL,

    -- The complete encoded hash, parameters included — the PHC string form, so
    -- a row records how it was hashed rather than depending on today's
    -- constants. Without that, raising the cost of the algorithm invalidates
    -- every password already stored.
    password_hash TEXT,

    -- No password. A person who signs in another way, or has not set one yet.
    -- Distinct from an empty string, which would compare against nothing and
    -- look like a valid failed attempt.
    CONSTRAINT users_password_hash_not_blank CHECK (password_hash IS NULL OR length(password_hash) > 0),

    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'closed')),

    -- Counted so repeated failures can be slowed, and cleared on success.
    failed_attempts INT NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,

    last_login_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

-- ---------------------------------------------------------------------------
-- What a person may do, and where
-- ---------------------------------------------------------------------------

-- A role is defined per tenant. A "manager" at a village society and a
-- "manager" at a processing plant authorise different things, and a shared
-- global definition would have to be the union of both.
CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(60) NOT NULL,
    description TEXT,

    -- The permissions this role carries, as a sorted array of strings. An array
    -- rather than a join table because a role's permissions are read together,
    -- always, and written rarely.
    permissions TEXT[] NOT NULL DEFAULT '{}',

    -- A role the platform defines and a tenant may not edit into something else.
    is_builtin BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,

    UNIQUE (tenant_id, name)
);

-- The membership below references a role by (tenant_id, id), not by id alone. A
-- single-column reference would let a membership in one tenant point at a role
-- defined in another: a foreign key is checked by the system rather than by the
-- querying role, so row-level security does not prevent it, and here that would
-- mean handing somebody another tenant's permissions. The composite reference
-- needs this key to point at, and it has to exist before the table that uses it.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'roles_tenant_id_key'
    ) THEN
        ALTER TABLE roles ADD CONSTRAINT roles_tenant_id_key UNIQUE (tenant_id, id);
    END IF;
END
$$;

-- Membership is the join between a person and a tenant, and it carries the role.
-- A person with no row here has no access to that tenant at all, which is the
-- default and the point.
CREATE TABLE IF NOT EXISTS tenant_memberships (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    user_id VARCHAR(26) NOT NULL REFERENCES users(id),
    role_id VARCHAR(26) NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended')),

    -- Which tenant this person lands in when they sign in without saying. Only
    -- one may be set per user; enforced by the partial index below.
    is_default BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,

    UNIQUE (tenant_id, user_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES roles(tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS tenant_memberships_one_default_per_user
    ON tenant_memberships (user_id) WHERE is_default AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Services calling services
-- ---------------------------------------------------------------------------

-- A service is not a person and must not borrow one's credentials. Giving the
-- ingestion service a user account means its actions are attributed to whoever
-- that account belongs to, and that its access cannot be withdrawn without
-- locking a person out.
CREATE TABLE IF NOT EXISTS service_identities (
    id VARCHAR(26) PRIMARY KEY,

    -- Null for a service acting across the platform — the settlement recomputer
    -- reconciling every tenant. Set for one issued to a single tenant.
    tenant_id VARCHAR(26),

    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,

    -- The secret, hashed the same way a password is. A service credential is a
    -- password that never gets rotated by a human noticing, so storing it in
    -- any recoverable form is worse, not better.
    secret_hash TEXT NOT NULL CHECK (length(secret_hash) > 0),

    permissions TEXT[] NOT NULL DEFAULT '{}',

    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'revoked')),

    -- A credential with no end date is one nobody ever rotates.
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

-- ---------------------------------------------------------------------------
-- Live sessions
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS auth_sessions (
    id VARCHAR(26) PRIMARY KEY,

    -- The tenant this session is acting for. A person who belongs to four
    -- tenants signs in to one of them at a time: the session names which, and
    -- every token minted from it carries that and nothing wider.
    tenant_id VARCHAR(26) NOT NULL,

    -- Exactly one of these. A session belongs to a person or to a service, and
    -- the difference has to survive into the audit trail.
    user_id VARCHAR(26) REFERENCES users(id),
    service_identity_id VARCHAR(26) REFERENCES service_identities(id),
    CONSTRAINT auth_sessions_one_subject CHECK (
        (user_id IS NOT NULL AND service_identity_id IS NULL) OR
        (user_id IS NULL AND service_identity_id IS NOT NULL)
    ),

    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,

    -- Set the moment the session stops being usable. Present rather than the row
    -- being deleted, because why a session ended is worth keeping.
    revoked_at TIMESTAMPTZ,
    revoked_reason VARCHAR(100),

    -- Where it was used from. Not for display: two continents in five minutes is
    -- how a stolen token announces itself.
    ip_address VARCHAR(45),
    user_agent TEXT,
    last_seen_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL
);

-- Verifying a token means finding its session, on every request. Restricted to
-- the sessions that could possibly be live, so the index stays small as expired
-- ones accumulate.
CREATE INDEX IF NOT EXISTS auth_sessions_live
    ON auth_sessions (id) WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS auth_sessions_by_user ON auth_sessions (user_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS auth_sessions_by_tenant ON auth_sessions (tenant_id, issued_at DESC);

-- ---------------------------------------------------------------------------
-- What actually happened
-- ---------------------------------------------------------------------------

-- Sign-ins, successful and not, kept whether or not the account existed.
--
-- The failures are the useful half. A hundred failures against ninety different
-- addresses is a credential-stuffing run and looks like nothing at all if only
-- successes are recorded.
CREATE TABLE IF NOT EXISTS authentication_attempts (
    id VARCHAR(26) PRIMARY KEY,

    -- What was presented, normalised. Not a foreign key: an attempt against an
    -- address that does not exist is exactly the kind worth keeping.
    email_normalised VARCHAR(320) NOT NULL,

    -- Set when the address did resolve to somebody.
    user_id VARCHAR(26) REFERENCES users(id),
    tenant_id VARCHAR(26),

    succeeded BOOLEAN NOT NULL,
    -- Why it failed, for the operator reading the log. Never returned to the
    -- caller, who is told only that the credentials were wrong: distinguishing
    -- "no such account" from "wrong password" tells an attacker which addresses
    -- are worth attacking.
    failure_reason VARCHAR(60),

    ip_address VARCHAR(45),
    user_agent TEXT,

    attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS authentication_attempts_by_email
    ON authentication_attempts (email_normalised, attempted_at DESC);
CREATE INDEX IF NOT EXISTS authentication_attempts_by_ip
    ON authentication_attempts (ip_address, attempted_at DESC) WHERE NOT succeeded;

CREATE INDEX IF NOT EXISTS roles_tenant ON roles (tenant_id);
CREATE INDEX IF NOT EXISTS tenant_memberships_user ON tenant_memberships (user_id);
CREATE INDEX IF NOT EXISTS service_identities_tenant ON service_identities (tenant_id);
