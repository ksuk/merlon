-- Run as the migration/schema owner. MERLON_APP_ROLE is the serving API
-- role and must not own audit_logs or rule_activation_events.
\set ON_ERROR_STOP on
\if :{?MERLON_APP_ROLE}
\else
\set MERLON_APP_ROLE merlon_app
\endif
REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON TABLE public.audit_logs FROM :MERLON_APP_ROLE;
GRANT SELECT, INSERT ON TABLE public.audit_logs TO :MERLON_APP_ROLE;
REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON TABLE public.rule_activation_events FROM :MERLON_APP_ROLE;
GRANT SELECT, INSERT ON TABLE public.rule_activation_events TO :MERLON_APP_ROLE;
