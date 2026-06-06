CREATE TABLE IF NOT EXISTS notifications (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    recipient_id VARCHAR(26) NOT NULL,
    recipient_type VARCHAR(30) NOT NULL DEFAULT 'user',
    channel VARCHAR(30) NOT NULL DEFAULT 'in_app',
    title VARCHAR(300) NOT NULL,
    body TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    priority VARCHAR(20) NOT NULL DEFAULT 'normal',
    reference_id VARCHAR(26),
    reference_type VARCHAR(50),
    sent_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS notification_templates (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    channel VARCHAR(30) NOT NULL,
    title VARCHAR(300) NOT NULL,
    body_template TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient ON notifications(tenant_id, recipient_id, status);
CREATE INDEX IF NOT EXISTS idx_notification_templates_tenant ON notification_templates(tenant_id, event_type);
