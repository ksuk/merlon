-- Retention periods are deployment-controlled for every retained data
-- category. Keep the original defaults, but remove the seeded lower bounds so
-- an administrator can select the period required by the customer's policy.
UPDATE retention_policies SET min_retention_days = NULL;

ALTER TABLE retention_policies
    ADD CONSTRAINT retention_days_positive CHECK (retention_days > 0);
