-- Run as the migration/schema owner after migrations or pg_restore.
--
-- MERLON_APP_ROLE is the serving API role. This procedure deliberately
-- classifies every application table instead of granting on ALL TABLES:
-- introducing a table therefore fails the classification test until its
-- serving-role access is reviewed.
\set ON_ERROR_STOP on
\if :{?MERLON_APP_ROLE}
\else
\set MERLON_APP_ROLE merlon_app
\endif

BEGIN;

-- psql quotes the value as a SQL literal before sending this statement.
-- Keeping the role in a session setting lets the DO block use format(%I) for
-- every identifier-bearing GRANT/REVOKE.
SELECT set_config('merlon.app_role', :'MERLON_APP_ROLE', false);

DO $merlon_app_grants$
DECLARE
    app_role text := current_setting('merlon.app_role');
    app_is_superuser boolean;
    database_owner text;
    executor_is_superuser boolean;
    table_name text;
    privilege_name text;
    forbidden_dml_privileges text[];
    forbidden_append_only_privileges text[];
    forbidden_ledger_privileges text[];
    dml_tables text[] := ARRAY[
        'account_customers',
        'accounts',
        'alerts',
        'api_keys',
        'backtest_job_customer_snapshots',
        'backtest_job_customers',
        'backtest_jobs',
        'batch_runs',
		'case_checklist_items',
		'case_notes',
		'case_relationships',
		'cases',
		'case_work_items',
        'customer_score_history',
        'customers',
        'domain_event_outbox',
        'pending_evaluations',
        'refresh_tokens',
        'retention_policies',
        'rule_definitions',
        'screening_list_failures',
        'screening_list_snapshots',
        'screening_results',
        'seed_state',
        'str_reports',
        'transactions',
        'users',
        'webhook_deliveries',
        'webhook_dlq',
        'webhooks',
        'whitelist_entries',
        'whitelist_reviews'
    ];
    append_only_tables text[] := ARRAY[
        'alert_decision_events',
        'audit_logs',
        'case_events',
        'case_evidence',
        'case_relationship_events',
        'rule_activation_events',
        'str_report_events'
    ];
BEGIN
    -- PostgreSQL 18 introduced the MAINTAIN table privilege. Keep this
    -- procedure runnable on the PostgreSQL 16/17 versions supported by the
    -- application while still rejecting it where the privilege exists.
    forbidden_dml_privileges := ARRAY['TRUNCATE', 'REFERENCES', 'TRIGGER'];
    forbidden_append_only_privileges := ARRAY[
        'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER'
    ];
    forbidden_ledger_privileges := ARRAY[
        'SELECT', 'INSERT', 'UPDATE', 'DELETE',
        'TRUNCATE', 'REFERENCES', 'TRIGGER'
    ];
    IF current_setting('server_version_num')::integer >= 180000 THEN
        forbidden_dml_privileges := forbidden_dml_privileges || ARRAY['MAINTAIN'];
        forbidden_append_only_privileges := forbidden_append_only_privileges || ARRAY['MAINTAIN'];
        forbidden_ledger_privileges := forbidden_ledger_privileges || ARRAY['MAINTAIN'];
    END IF;

    SELECT rolsuper
      INTO app_is_superuser
      FROM pg_catalog.pg_roles
     WHERE rolname = app_role;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'MERLON_APP_ROLE % does not exist', app_role;
    END IF;
    IF app_is_superuser THEN
        RAISE EXCEPTION 'MERLON_APP_ROLE % must not be a superuser', app_role;
    END IF;
    -- Database CREATE permits arbitrary persistent schemas. A direct REVOKE
    -- from the serving role cannot remove membership-derived access, so reject
    -- the effective privilege before normalizing schema and object grants.
    IF has_database_privilege(app_role, current_database(), 'CREATE') THEN
        RAISE EXCEPTION
            'MERLON_APP_ROLE % has forbidden CREATE on database %',
            app_role, current_database();
    END IF;
    IF NOT has_database_privilege(app_role, current_database(), 'CONNECT') THEN
        SELECT owner_role.rolname, executor_role.rolsuper
          INTO database_owner, executor_is_superuser
          FROM pg_catalog.pg_database AS db
          JOIN pg_catalog.pg_roles AS owner_role
            ON owner_role.oid = db.datdba
          JOIN pg_catalog.pg_roles AS executor_role
            ON executor_role.rolname = current_user
         WHERE db.datname = current_database();
        IF current_user <> database_owner AND NOT executor_is_superuser THEN
            RAISE EXCEPTION
                'MERLON_APP_ROLE % lacks CONNECT on database %; grant it as database owner % before hardening',
                app_role, current_database(), database_owner;
        END IF;
        EXECUTE format(
            'GRANT CONNECT ON DATABASE %I TO %I',
            current_database(),
            app_role
        );
    END IF;
    IF EXISTS (
        SELECT 1
          FROM pg_catalog.pg_tables
         WHERE schemaname = 'public'
           AND pg_has_role(app_role, tableowner, 'MEMBER')
    ) THEN
        RAISE EXCEPTION
            'MERLON_APP_ROLE % owns or inherits ownership of an application table',
            app_role;
    END IF;

    -- Schema DDL remains owner-only. Revoking PUBLIC matters on databases
    -- created with older PostgreSQL defaults.
    REVOKE CREATE ON SCHEMA public FROM PUBLIC;
    EXECUTE format('REVOKE CREATE ON SCHEMA public FROM %I', app_role);
    EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', app_role);

    -- Normalize every ordinary table to CRUD only. Tables absent from an older
    -- backup are skipped here, then classified when migrations create them and
    -- the operator reruns this procedure.
    FOREACH table_name IN ARRAY dml_tables LOOP
        IF to_regclass(format('%I.%I', 'public', table_name)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format(
            'REVOKE ALL PRIVILEGES ON TABLE public.%I FROM PUBLIC',
            table_name
        );
        EXECUTE format(
            'REVOKE ALL PRIVILEGES ON TABLE public.%I FROM %I',
            table_name,
            app_role
        );
        EXECUTE format(
            'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.%I TO %I',
            table_name,
            app_role
        );
    END LOOP;

    -- Audit and maker-checker evidence is append-only to the serving role.
    FOREACH table_name IN ARRAY append_only_tables LOOP
        IF to_regclass(format('%I.%I', 'public', table_name)) IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format(
            'REVOKE ALL PRIVILEGES ON TABLE public.%I FROM PUBLIC',
            table_name
        );
        EXECUTE format(
            'REVOKE ALL PRIVILEGES ON TABLE public.%I FROM %I',
            table_name,
            app_role
        );
        EXECUTE format(
            'GRANT SELECT, INSERT ON TABLE public.%I TO %I',
            table_name,
            app_role
        );
    END LOOP;

    -- BIGSERIAL on audit_logs calls nextval during an otherwise permitted
    -- INSERT; pg_dump/pg_restore --no-privileges removes this grant too.
    IF to_regclass('public.audit_logs_id_seq') IS NOT NULL THEN
        REVOKE ALL PRIVILEGES ON SEQUENCE public.audit_logs_id_seq FROM PUBLIC;
        EXECUTE format(
            'REVOKE ALL PRIVILEGES ON SEQUENCE public.audit_logs_id_seq FROM %I',
            app_role
        );
        EXECUTE format(
            'GRANT USAGE ON SEQUENCE public.audit_logs_id_seq TO %I',
            app_role
        );
    END IF;

    -- The migration ledger is operator-only.
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        REVOKE ALL PRIVILEGES ON TABLE public.schema_migrations FROM PUBLIC;
        EXECUTE format(
            'REVOKE ALL PRIVILEGES ON TABLE public.schema_migrations FROM %I',
            app_role
        );
    END IF;

    -- Fail closed if membership in another role still supplies forbidden
    -- privileges that a direct REVOKE cannot remove.
    FOREACH table_name IN ARRAY dml_tables LOOP
        IF to_regclass(format('%I.%I', 'public', table_name)) IS NULL THEN
            CONTINUE;
        END IF;
        FOREACH privilege_name IN ARRAY forbidden_dml_privileges LOOP
            IF has_table_privilege(
                app_role,
                format('%I.%I', 'public', table_name),
                privilege_name
            ) THEN
                RAISE EXCEPTION
                    'MERLON_APP_ROLE % inherits forbidden % on public.%',
                    app_role, privilege_name, table_name;
            END IF;
        END LOOP;
    END LOOP;
    FOREACH table_name IN ARRAY append_only_tables LOOP
        IF to_regclass(format('%I.%I', 'public', table_name)) IS NULL THEN
            CONTINUE;
        END IF;
        FOREACH privilege_name IN ARRAY forbidden_append_only_privileges LOOP
            IF has_table_privilege(
                app_role,
                format('%I.%I', 'public', table_name),
                privilege_name
            ) THEN
                RAISE EXCEPTION
                    'MERLON_APP_ROLE % inherits forbidden % on public.%',
                    app_role, privilege_name, table_name;
            END IF;
        END LOOP;
    END LOOP;
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        FOREACH privilege_name IN ARRAY forbidden_ledger_privileges LOOP
            IF has_table_privilege(
                app_role,
                'public.schema_migrations',
                privilege_name
            ) THEN
                RAISE EXCEPTION
                    'MERLON_APP_ROLE % inherits % on migration ledger',
                    app_role, privilege_name;
            END IF;
        END LOOP;
    END IF;
    IF to_regclass('public.audit_logs_id_seq') IS NOT NULL THEN
        FOREACH privilege_name IN ARRAY ARRAY['SELECT', 'UPDATE'] LOOP
            IF has_sequence_privilege(
                app_role,
                'public.audit_logs_id_seq',
                privilege_name
            ) THEN
                RAISE EXCEPTION
                    'MERLON_APP_ROLE % inherits forbidden % on audit sequence',
                    app_role, privilege_name;
            END IF;
        END LOOP;
    END IF;
    IF has_schema_privilege(app_role, 'public', 'CREATE') THEN
        RAISE EXCEPTION
            'MERLON_APP_ROLE % inherits CREATE on public schema',
            app_role;
    END IF;
END
$merlon_app_grants$;

COMMIT;
