-- WS-2: add COUNTRY_RISK rule type (the rule schema §3.5)
-- ALTER TYPE ... ADD VALUE cannot be used in the same transaction as a
-- statement that references the new value, so this migration only adds the
-- enum value and touches nothing else.
ALTER TYPE rule_type ADD VALUE IF NOT EXISTS 'COUNTRY_RISK';
