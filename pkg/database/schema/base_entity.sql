-- ============================================================================
-- BASE ENTITY SCHEMA
-- Location: packages/database/schema/base_entity.sql
-- Purpose: Single source of truth for multi-tenant base entity and RLS
-- ============================================================================
-- This file MUST be executed ONCE in the public schema before any service schemas.
-- All domain tables should inherit from public.base_entity.
-- ============================================================================

-- Ensure public schema exists
CREATE SCHEMA IF NOT EXISTS public;

-- ============================================================================
-- ULID SUPPORT
-- ============================================================================
-- We depend on a gen_ulid() function that returns CHAR(26). Two options:
--   1. Third-party `pg_ulid` extension (not shipped with Postgres by default).
--   2. Polyfill at packages/database/schema/gen_ulid_polyfill.sql (uses only
--      the stock `pgcrypto` extension; matches packages/ulid output format).
--
-- Run the polyfill BEFORE this file if pg_ulid is not installed. The migration
-- CLI bootstrap executes gen_ulid_polyfill.sql first, so operators using the
-- standard migration path need no manual step.
-- ============================================================================

-- ============================================================================
-- BASE ENTITY TABLE
-- ============================================================================
-- This is an ABSTRACT base table. It must never be queried directly.
-- All domain tables inherit from this table using PostgreSQL table inheritance.

CREATE TABLE IF NOT EXISTS public.base_entity (
    -- primary identity
    id CHAR(26) PRIMARY KEY DEFAULT gen_ulid(),

    -- multi-tenancy
    tenant_id  CHAR(26) NOT NULL,
    company_id CHAR(26) NOT NULL,
    branch_id  CHAR(26) NOT NULL,

    -- auditing
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by CHAR(26) NOT NULL,

    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by CHAR(26),

    -- soft delete
    deleted_at TIMESTAMPTZ,
    deleted_by CHAR(26),

    -- soft delete integrity
    CONSTRAINT chk_soft_delete
      CHECK (
        deleted_at IS NULL OR deleted_by IS NOT NULL
      )
);

-- Comment for documentation
COMMENT ON TABLE public.base_entity IS 'Abstract base table for multi-tenant entities. Never query directly. All domain tables inherit from this.';

-- RLS_DISABLED: This is the abstract base table - RLS is applied to child tables only
-- Do NOT enable RLS on public.base_entity

-- ============================================================================
-- SHARED TRIGGER FUNCTIONS
-- ============================================================================

-- Function: Update updated_at timestamp on row modification
CREATE OR REPLACE FUNCTION public.trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.trigger_set_updated_at() IS 'Trigger function to automatically update updated_at timestamp on UPDATE';

-- Function: Soft delete (sets deleted_at and deleted_by)
CREATE OR REPLACE FUNCTION public.trigger_soft_delete()
RETURNS TRIGGER AS $$
BEGIN
    -- If deleted_at is being set and was previously NULL, record deletion
    IF NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL THEN
        NEW.deleted_at = NOW();
        -- deleted_by should be set by the application, but we ensure updated_at is also set
        NEW.updated_at = NOW();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.trigger_soft_delete() IS 'Trigger function to handle soft delete timestamp';

-- Function: Prevent modification of deleted rows
CREATE OR REPLACE FUNCTION public.trigger_prevent_update_deleted()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'Cannot modify a deleted record (id: %)', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.trigger_prevent_update_deleted() IS 'Trigger function to prevent updates to soft-deleted records';

-- ============================================================================
-- HELPER FUNCTION: Apply base entity triggers to a child table
-- ============================================================================
-- Usage: SELECT public.apply_base_entity_triggers('schema_name', 'table_name');

CREATE OR REPLACE FUNCTION public.apply_base_entity_triggers(
    p_schema_name TEXT,
    p_table_name TEXT
) RETURNS VOID AS $$
DECLARE
    v_full_table_name TEXT;
BEGIN
    v_full_table_name := quote_ident(p_schema_name) || '.' || quote_ident(p_table_name);

    -- Trigger 1: Automatically update updated_at on any UPDATE
    -- Note: updated_by must be set by the application in the query
    EXECUTE format(
        'DROP TRIGGER IF EXISTS trg_%I_updated_at ON %s',
        p_table_name, v_full_table_name
    );
    EXECUTE format(
        'CREATE TRIGGER trg_%I_updated_at
         BEFORE UPDATE ON %s
         FOR EACH ROW
         EXECUTE FUNCTION public.trigger_set_updated_at()',
        p_table_name, v_full_table_name
    );

    -- Trigger 2: Handle soft delete (normalize deleted_at timestamp)
    -- Note: deleted_by must be set by the application in the query
    EXECUTE format(
        'DROP TRIGGER IF EXISTS trg_%I_soft_delete ON %s',
        p_table_name, v_full_table_name
    );
    EXECUTE format(
        'CREATE TRIGGER trg_%I_soft_delete
         BEFORE UPDATE ON %s
         FOR EACH ROW
         WHEN (NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL)
         EXECUTE FUNCTION public.trigger_soft_delete()',
        p_table_name, v_full_table_name
    );

    -- Trigger 3: Prevent modification of soft-deleted records
    -- This ensures data integrity by blocking updates to deleted rows
    EXECUTE format(
        'DROP TRIGGER IF EXISTS trg_%I_prevent_update_deleted ON %s',
        p_table_name, v_full_table_name
    );
    EXECUTE format(
        'CREATE TRIGGER trg_%I_prevent_update_deleted
         BEFORE UPDATE ON %s
         FOR EACH ROW
         EXECUTE FUNCTION public.trigger_prevent_update_deleted()',
        p_table_name, v_full_table_name
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.apply_base_entity_triggers(TEXT, TEXT) IS 'Helper function to apply standard base entity triggers to a child table';

-- ============================================================================
-- HELPER FUNCTION: Apply RLS policies to a child table
-- ============================================================================
-- Usage: SELECT public.apply_rls_policies('schema_name', 'table_name');

CREATE OR REPLACE FUNCTION public.apply_rls_policies(
    p_schema_name TEXT,
    p_table_name TEXT
) RETURNS VOID AS $$
DECLARE
    v_full_table_name TEXT;
BEGIN
    v_full_table_name := quote_ident(p_schema_name) || '.' || quote_ident(p_table_name);

    -- Enable RLS
    EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', v_full_table_name);
    EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', v_full_table_name);

    -- Drop existing policies if they exist
    EXECUTE format('DROP POLICY IF EXISTS tenant_read_policy ON %s', v_full_table_name);
    EXECUTE format('DROP POLICY IF EXISTS tenant_write_policy ON %s', v_full_table_name);

    -- SELECT policy (read): tenant + company + branch isolation + soft delete filter
    EXECUTE format(
        'CREATE POLICY tenant_read_policy ON %s
         FOR SELECT
         USING (
             tenant_id = current_setting(''app.tenant_id'')::char(26)
             AND company_id = current_setting(''app.company_id'')::char(26)
             AND branch_id = current_setting(''app.branch_id'')::char(26)
             AND deleted_at IS NULL
         )',
        v_full_table_name
    );

    -- INSERT/UPDATE policy (write): tenant + company + branch isolation
    EXECUTE format(
        'CREATE POLICY tenant_write_policy ON %s
         FOR INSERT
         WITH CHECK (
             tenant_id = current_setting(''app.tenant_id'')::char(26)
             AND company_id = current_setting(''app.company_id'')::char(26)
             AND branch_id = current_setting(''app.branch_id'')::char(26)
         )',
        v_full_table_name
    );

    EXECUTE format(
        'CREATE POLICY tenant_update_policy ON %s
         FOR UPDATE
         USING (
             tenant_id = current_setting(''app.tenant_id'')::char(26)
             AND company_id = current_setting(''app.company_id'')::char(26)
             AND branch_id = current_setting(''app.branch_id'')::char(26)
         )
         WITH CHECK (
             tenant_id = current_setting(''app.tenant_id'')::char(26)
             AND company_id = current_setting(''app.company_id'')::char(26)
             AND branch_id = current_setting(''app.branch_id'')::char(26)
         )',
        v_full_table_name
    );

    -- DELETE policy (for actual deletes, though soft delete is preferred)
    EXECUTE format(
        'CREATE POLICY tenant_delete_policy ON %s
         FOR DELETE
         USING (
             tenant_id = current_setting(''app.tenant_id'')::char(26)
             AND company_id = current_setting(''app.company_id'')::char(26)
             AND branch_id = current_setting(''app.branch_id'')::char(26)
         )',
        v_full_table_name
    );
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.apply_rls_policies(TEXT, TEXT) IS 'Helper function to apply standard RLS policies to a child table';

-- ============================================================================
-- HELPER FUNCTION: Full setup for child table (triggers + RLS)
-- ============================================================================
-- Usage: SELECT public.setup_base_entity_child('schema_name', 'table_name');

CREATE OR REPLACE FUNCTION public.setup_base_entity_child(
    p_schema_name TEXT,
    p_table_name TEXT
) RETURNS VOID AS $$
BEGIN
    -- Apply triggers
    PERFORM public.apply_base_entity_triggers(p_schema_name, p_table_name);

    -- Apply RLS policies
    PERFORM public.apply_rls_policies(p_schema_name, p_table_name);
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION public.setup_base_entity_child(TEXT, TEXT) IS 'Complete setup for a child table: applies triggers and RLS policies';

-- ============================================================================
-- END OF BASE ENTITY SCHEMA
-- ============================================================================
