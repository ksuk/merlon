-- M3: Add missing columns for full domain model support

ALTER TABLE customers ADD COLUMN IF NOT EXISTS country_code VARCHAR(2) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS product_types TEXT[] NOT NULL DEFAULT '{}';
