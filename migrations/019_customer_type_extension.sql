-- WS-11 Task3: non-natural-person customer_type extension (the data model
-- §1.1.1). ALTER TYPE ... ADD VALUE cannot run in the same transaction as
-- other DDL that might reference the new value, so this is a standalone
-- migration file (see 018_customer_lifecycle.sql for the status column).

ALTER TYPE customer_type ADD VALUE IF NOT EXISTS 'trust';
ALTER TYPE customer_type ADD VALUE IF NOT EXISTS 'partnership';
ALTER TYPE customer_type ADD VALUE IF NOT EXISTS 'npo';
ALTER TYPE customer_type ADD VALUE IF NOT EXISTS 'government';
ALTER TYPE customer_type ADD VALUE IF NOT EXISTS 'foreign_legal_arrangement';
