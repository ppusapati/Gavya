-- ============================================================================
-- BASE ENTITY SCHEMA (SQLC Compatible Version)
-- Location: packages/database/schema/base_entity_sqlc.sql
-- Purpose: SQLC-compatible version of base_entity for code generation
-- ============================================================================
-- This file is used ONLY by sqlc for code generation.
-- The actual database uses base_entity.sql with pg_ulid extension.
-- ============================================================================

-- Ensure public schema exists
CREATE SCHEMA IF NOT EXISTS public;

-- ============================================================================
-- BASE ENTITY TABLE (SQLC Compatible)
-- ============================================================================
-- Using CHAR(26) instead of CHAR(26) for sqlc compatibility

CREATE TABLE IF NOT EXISTS public.base_entity (
    -- primary identity
    id CHAR(26) PRIMARY KEY,

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

-- ============================================================================
-- STUB FUNCTIONS (for SQLC parsing only)
-- ============================================================================

CREATE OR REPLACE FUNCTION public.apply_base_entity_triggers(
    p_schema_name TEXT,
    p_table_name TEXT
) RETURNS VOID AS $$
BEGIN
    -- Stub for sqlc
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION public.apply_rls_policies(
    p_schema_name TEXT,
    p_table_name TEXT
) RETURNS VOID AS $$
BEGIN
    -- Stub for sqlc
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION public.setup_base_entity_child(
    p_schema_name TEXT,
    p_table_name TEXT
) RETURNS VOID AS $$
BEGIN
    -- Stub for sqlc
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- END OF SQLC COMPATIBLE BASE ENTITY SCHEMA
-- ============================================================================
