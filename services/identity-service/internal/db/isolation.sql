-- Isolation for the identity tables, which the general sweep cannot get right.
--
-- Two problems, neither of which appears anywhere else in the schema.
--
--
-- THE TABLE WITH NO TENANT COLUMN
--
-- `users` is deliberately global: a federation manager oversees a dozen
-- societies and a vet works across four, and one login per tenant means one
-- password reset per tenant and no way to say who a person is across the estate.
-- So there is no tenant_id to compare against, and the column-driven sweep in
-- libs/integrity/isolation leaves the table alone — which would let any tenant
-- read every user's address and password hash.
--
-- It is isolated by membership instead. A tenant sees a person exactly when that
-- person is one of theirs, which is a policy the database can enforce and
-- happens to be the answer to the business question too.
--
--
-- THE QUERIES THAT RUN BEFORE A TENANT IS KNOWN
--
-- Authentication is the one thing that cannot already know its tenant: finding
-- the account for an email address, and resolving a session to the tenant it
-- acts for, are what establish the tenant in the first place. Under the
-- policies, those queries are refused — and the refusal is correct, because
-- there is genuinely no tenant yet.
--
-- The tempting fix is to exempt the identity tables, or to give the identity
-- service a role that bypasses the policies. Both hand it unrestricted read
-- across every tenant's memberships and sessions for the sake of three lookups.
--
-- Instead there are six functions, each running with the definer's rights, each
-- doing exactly one thing the pre-authentication path needs and no more.
-- Everything else the identity service does goes through the policies like every
-- other service. The exception is six short functions rather than a role that
-- can read everything.
--
-- Three of them read: the account for an address, the tenants a person may sign
-- in to, and the credential for a service. Three write, and the reason is the
-- same in each case — the write happens before a tenant exists. Recording a
-- failed sign-in against an address that has no account is the clearest: those
-- are the attempts most worth keeping, they have no tenant to be scoped to, and
-- under the policy the insert is simply refused, so a credential-stuffing run
-- would leave no trace at all.

-- ---------------------------------------------------------------------------
-- users, isolated by membership
-- ---------------------------------------------------------------------------

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON users;

CREATE POLICY tenant_isolation ON users
FOR ALL
USING (
    EXISTS (
        SELECT 1 FROM tenant_memberships m
        WHERE m.user_id = users.id
          AND m.tenant_id = gavya_current_tenant()
          AND m.deleted_at IS NULL
    )
)
-- Writing is narrower than reading. A tenant may update somebody who is already
-- one of theirs; creating a person is not a tenant-scoped act, because the
-- membership that would authorise it does not exist yet. That path goes through
-- the definer-rights function below, where the caller has to say which tenant
-- the new person is being added to.
WITH CHECK (
    EXISTS (
        SELECT 1 FROM tenant_memberships m
        WHERE m.user_id = users.id
          AND m.tenant_id = gavya_current_tenant()
          AND m.deleted_at IS NULL
    )
);

-- ---------------------------------------------------------------------------
-- The three pre-authentication lookups
-- ---------------------------------------------------------------------------

-- gavya_find_user_for_login answers "is there an account for this address, and
-- what does verifying it need".
--
-- It returns the hash rather than doing the comparison, because comparing is the
-- application's job and the algorithm lives there. It returns a row even for an
-- address with no account — with found = false and a dummy-shaped result — so
-- the caller can do the same work either way. A caller that returns early on a
-- missing account is measurably faster in that case, and the difference tells an
-- attacker which addresses exist.
CREATE OR REPLACE FUNCTION gavya_find_user_for_login(p_email_normalised text)
RETURNS TABLE(
    found         boolean,
    user_id       text,
    password_hash text,
    status        text,
    locked_until  timestamptz,
    failed_attempts int
) AS $fn$
BEGIN
    RETURN QUERY
    SELECT true, u.id::text, u.password_hash, u.status::text, u.locked_until, u.failed_attempts
    FROM users u
    WHERE u.email_normalised = p_email_normalised
      AND u.deleted_at IS NULL;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, NULL::text, NULL::text, NULL::text, NULL::timestamptz, 0;
    END IF;
END
$fn$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

COMMENT ON FUNCTION gavya_find_user_for_login(text) IS
    'Pre-authentication lookup: the stored credential for an address, or a not-found row of the same shape.';

-- gavya_resolve_session answers "what tenant is this session acting for, and is
-- it still usable".
--
-- This is what makes a signed token withdrawable. A token checked only by its
-- signature is valid until it expires, so dismissal, credential theft and a lost
-- laptop are all unhandleable for the length of the lifetime. One indexed lookup
-- per request buys revocation that takes effect on the next request.
--
-- It says why a session is unusable, because "expired" and "revoked" call for
-- different things from whoever is reading the logs — but the caller must not
-- pass that distinction to the client, who is told only that they are not signed
-- in.
CREATE OR REPLACE FUNCTION gavya_resolve_session(p_session_id text)
RETURNS TABLE(
    valid               boolean,
    reason              text,
    tenant_id           text,
    user_id             text,
    service_identity_id text
) AS $fn$
DECLARE
    s record;
BEGIN
    SELECT * INTO s FROM auth_sessions a WHERE a.id = p_session_id;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'no such session', NULL::text, NULL::text, NULL::text;
        RETURN;
    END IF;
    IF s.revoked_at IS NOT NULL THEN
        RETURN QUERY SELECT false, coalesce(s.revoked_reason, 'revoked')::text,
                            NULL::text, NULL::text, NULL::text;
        RETURN;
    END IF;
    IF s.expires_at <= now() THEN
        RETURN QUERY SELECT false, 'expired', NULL::text, NULL::text, NULL::text;
        RETURN;
    END IF;

    RETURN QUERY SELECT true, NULL::text, s.tenant_id::text,
                        s.user_id::text, s.service_identity_id::text;
END
$fn$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

COMMENT ON FUNCTION gavya_resolve_session(text) IS
    'Pre-authentication lookup: the tenant a session acts for, and whether it is still live.';

-- gavya_find_service_identity answers the same question for a service.
--
-- A service credential is a password nobody notices needs rotating, so the
-- expiry is part of what is checked here rather than left to whoever remembers.
CREATE OR REPLACE FUNCTION gavya_find_service_identity(p_name text)
RETURNS TABLE(
    found       boolean,
    identity_id text,
    tenant_id   text,
    secret_hash text,
    status      text,
    expires_at  timestamptz,
    permissions text[]
) AS $fn$
BEGIN
    RETURN QUERY
    SELECT true, si.id::text, si.tenant_id::text, si.secret_hash, si.status::text,
           si.expires_at, si.permissions
    FROM service_identities si
    WHERE si.name = p_name AND si.deleted_at IS NULL;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, NULL::text, NULL::text, NULL::text, NULL::text,
                            NULL::timestamptz, NULL::text[];
    END IF;
END
$fn$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

COMMENT ON FUNCTION gavya_find_service_identity(text) IS
    'Pre-authentication lookup: the stored credential for a service, or a not-found row of the same shape.';


-- gavya_memberships_for_login answers "which tenants may this person sign in
-- to". It spans tenants by necessity: the answer is what decides which one the
-- session will be scoped to, so it cannot be asked from inside one.
--
-- Keyed by user id rather than by address, so it can only be asked about
-- somebody whose account has already been found.
CREATE OR REPLACE FUNCTION gavya_memberships_for_login(p_user_id text)
RETURNS TABLE(
    tenant_id  text,
    role_id    text,
    role_name  text,
    status     text,
    is_default boolean
) AS $fn$
BEGIN
    RETURN QUERY
    SELECT m.tenant_id::text, m.role_id::text, r.name::text, m.status::text, m.is_default
    FROM tenant_memberships m
    JOIN roles r ON r.tenant_id = m.tenant_id AND r.id = m.role_id
    WHERE m.user_id = p_user_id
      AND m.deleted_at IS NULL
    ORDER BY m.tenant_id;
END
$fn$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

COMMENT ON FUNCTION gavya_memberships_for_login(text) IS
    'Pre-authentication lookup: the tenants a person may sign in to.';

-- gavya_record_authentication_attempt writes one line of the sign-in log.
--
-- It needs definer rights for the same reason the log is worth keeping: the
-- attempts that matter most are the ones against addresses that do not exist,
-- and those have no tenant to be scoped to. Under the policy that insert is
-- refused — so a credential-stuffing run would leave no trace at all, which is
-- the exact opposite of what the table is for.
--
-- Reading the log stays under the policy: a tenant sees its own attempts and not
-- anybody else's, since the rows carry email addresses.
CREATE OR REPLACE FUNCTION gavya_record_authentication_attempt(
    p_id               text,
    p_email_normalised text,
    p_user_id          text,
    p_tenant_id        text,
    p_succeeded        boolean,
    p_failure_reason   text,
    p_ip_address       text,
    p_user_agent       text
) RETURNS void AS $fn$
BEGIN
    INSERT INTO authentication_attempts
        (id, email_normalised, user_id, tenant_id, succeeded, failure_reason, ip_address, user_agent)
    VALUES
        (p_id, p_email_normalised, nullif(p_user_id, ''), nullif(p_tenant_id, ''),
         p_succeeded, nullif(p_failure_reason, ''), nullif(p_ip_address, ''), nullif(p_user_agent, ''));
END
$fn$ LANGUAGE plpgsql SECURITY DEFINER;

COMMENT ON FUNCTION gavya_record_authentication_attempt(text,text,text,text,boolean,text,text,text) IS
    'Records one sign-in attempt, including against addresses that have no account and therefore no tenant.';

-- gavya_record_login_state writes back what an attempt did to an account: the
-- failure count and any lock. Same reason as above — it happens before a tenant
-- exists, and `users` has none of its own.
CREATE OR REPLACE FUNCTION gavya_record_login_state(
    p_user_id         text,
    p_failed_attempts int,
    p_locked_until    timestamptz,
    p_succeeded       boolean
) RETURNS void AS $fn$
BEGIN
    UPDATE users
    SET failed_attempts = p_failed_attempts,
        locked_until    = p_locked_until,
        last_login_at   = CASE WHEN p_succeeded THEN now() ELSE last_login_at END,
        updated_at      = now()
    WHERE id = p_user_id;
END
$fn$ LANGUAGE plpgsql SECURITY DEFINER;

COMMENT ON FUNCTION gavya_record_login_state(text,int,timestamptz,boolean) IS
    'Writes back the failure count and lock an attempt produced.';

-- ---------------------------------------------------------------------------
-- Who may call them
-- ---------------------------------------------------------------------------

-- These run with the definer's rights, so who may call them is the whole
-- control. Revoked from PUBLIC first: a function created by a superuser is
-- executable by everybody by default, which would make each of these a hole
-- rather than a door.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_app') THEN
        REVOKE ALL ON FUNCTION gavya_find_user_for_login(text) FROM PUBLIC;
        REVOKE ALL ON FUNCTION gavya_resolve_session(text) FROM PUBLIC;
        REVOKE ALL ON FUNCTION gavya_find_service_identity(text) FROM PUBLIC;

        REVOKE ALL ON FUNCTION gavya_memberships_for_login(text) FROM PUBLIC;
        REVOKE ALL ON FUNCTION gavya_record_authentication_attempt(text,text,text,text,boolean,text,text,text) FROM PUBLIC;
        REVOKE ALL ON FUNCTION gavya_record_login_state(text,int,timestamptz,boolean) FROM PUBLIC;

        GRANT EXECUTE ON FUNCTION gavya_find_user_for_login(text) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_resolve_session(text) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_find_service_identity(text) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_memberships_for_login(text) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_record_authentication_attempt(text,text,text,text,boolean,text,text,text) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_record_login_state(text,int,timestamptz,boolean) TO gavya_app;
    END IF;
END
$$;

-- ---------------------------------------------------------------------------
-- Adding a person to a tenant
-- ---------------------------------------------------------------------------

-- gavya_add_member creates a person, or attaches an existing one, and gives them
-- a role in one tenant.
--
-- Definer rights, and the policy on users above says why: a tenant may update
-- somebody who is already one of theirs, and creating a person is not a
-- tenant-scoped act, because the membership that would authorise it does not
-- exist yet. The comment there has pointed at "the definer-rights function
-- below" since the table was written and there was no such function, which is
-- why a co-operative could not be given a single clerk without a database
-- console.
--
-- The tenant is a parameter rather than gavya_current_tenant() because this runs
-- with the privileges of its owner and must not be able to read which tenant it
-- is in from ambient state. The caller says which tenant, and the caller has
-- already been authorised for tenant.admin against that tenant.
--
-- An address that already has an account joins the existing person rather than
-- creating a second one: two accounts for one address is how a revoked login
-- stays usable, which is the reason email_normalised is unique in the first
-- place.
CREATE OR REPLACE FUNCTION gavya_add_member(
    p_tenant        text,
    p_email         text,
    p_full_name     text,
    p_password_hash text,
    p_role_id       text,
    p_actor         text,
    p_membership_id text,
    p_user_id       text
)
-- The output columns are prefixed because plpgsql resolves an unqualified name
-- against them before it resolves it against a table: RETURNS TABLE(user_id ...)
-- makes ON CONFLICT (tenant_id, user_id) below ambiguous, and Postgres refuses
-- it rather than guessing.
RETURNS TABLE(out_user_id text, out_created boolean) AS $fn$
DECLARE
    existing text;
    made     boolean := false;
BEGIN
    IF p_tenant = '' OR p_tenant IS NULL THEN
        RAISE EXCEPTION 'a member must be added to a tenant';
    END IF;

    SELECT u.id INTO existing FROM users u WHERE u.email_normalised = lower(p_email);

    IF existing IS NULL THEN
        INSERT INTO users (id, email, email_normalised, full_name, password_hash,
                           created_by, updated_by)
        VALUES (p_user_id, p_email, lower(p_email), p_full_name,
                nullif(p_password_hash, ''), p_actor, p_actor);
        existing := p_user_id;
        made := true;
    END IF;

    -- A second membership in the same tenant is the same membership. Restoring
    -- one that was removed keeps the row and its history rather than leaving a
    -- deleted row beside a live one for the same pair.
    INSERT INTO tenant_memberships (id, tenant_id, user_id, role_id, created_by, updated_by)
    VALUES (p_membership_id, p_tenant, existing, p_role_id, p_actor, p_actor)
    ON CONFLICT (tenant_id, user_id) DO UPDATE
        SET role_id    = EXCLUDED.role_id,
            status     = 'active',
            deleted_at = NULL,
            updated_at = NOW(),
            updated_by = EXCLUDED.updated_by;

    RETURN QUERY SELECT existing, made;
END
$fn$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER;

REVOKE ALL ON FUNCTION gavya_add_member(text, text, text, text, text, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION gavya_add_member(text, text, text, text, text, text, text, text) TO gavya_app;
