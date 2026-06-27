-- Merlon Initial Schema
-- M1.1: Core tables for CDD scoring foundation

-- Custom types
CREATE TYPE customer_type AS ENUM ('individual', 'corporate_domestic', 'corporate_foreign');
CREATE TYPE risk_tier AS ENUM ('low', 'medium', 'high');
CREATE TYPE rule_type AS ENUM ('TM_SCENARIO', 'CDD_WEIGHT', 'SCREENING_CONFIG');

-- Customers
-- Future: tenant_id column for multi-tenant support
-- Future: PII fields (name, address, date_of_birth) will be AES-256-GCM encrypted at application level
CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id VARCHAR(255) NOT NULL,
    customer_type customer_type NOT NULL,
    attributes JSONB NOT NULL DEFAULT '{}',
    risk_score DECIMAL(5,2),
    risk_tier risk_tier,
    last_scored_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customers_external_id_unique UNIQUE (external_id)
);

CREATE INDEX idx_customers_risk_tier ON customers (risk_tier);
CREATE INDEX idx_customers_last_scored_at ON customers (last_scored_at);

-- Customer Score History (append-only, supports Auditability First principle)
CREATE TABLE customer_score_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(id),
    score DECIMAL(5,2) NOT NULL,
    tier risk_tier NOT NULL,
    factors JSONB NOT NULL DEFAULT '{}',
    rule_set_id VARCHAR(255) NOT NULL,
    rule_set_version INTEGER NOT NULL,
    scored_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_score_history_customer_scored ON customer_score_history (customer_id, scored_at);

-- Rule Definitions (versioned, supports Configuration as the Product principle)
CREATE TABLE rule_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type rule_type NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    definition JSONB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rule_definitions_name_version_unique UNIQUE (name, version)
);

CREATE INDEX idx_rule_definitions_type_active ON rule_definitions (type, is_active);

-- Audit Logs (append-only: no UPDATE or DELETE operations on this table)
-- Future: integrity_hash column for Enterprise WORM tamper detection (hash chain)
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id VARCHAR(255),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(100) NOT NULL,
    resource_id VARCHAR(255),
    details JSONB DEFAULT '{}',
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at);
CREATE INDEX idx_audit_logs_resource ON audit_logs (resource_type, resource_id);
CREATE INDEX idx_audit_logs_user ON audit_logs (user_id, created_at);
