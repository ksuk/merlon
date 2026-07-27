#!/usr/bin/env python3
"""Opt-in PostgreSQL 18 integration tests for operational entry points."""

from __future__ import annotations

import os
from pathlib import Path
import re
import shutil
import subprocess
import time
import unittest
import uuid


ROOT = Path(__file__).resolve().parents[1]
RUN_INTEGRATION = os.environ.get("MERLON_RUN_POSTGRES_OPERATIONS_INTEGRATION") == "1"

# Set by the CI job that exists to exercise these scripts. Skipping is the
# right outcome for a developer without Docker; it is never the right outcome
# for that job. Without this, a runner image without Docker, a self-hosted
# runner, or a typo in the opt-in variable above turns the whole backup and
# restore surface green while testing nothing -- "Ran 0 tests ... OK", exit 0.
# Same principle check-env-vars.sh and check-openapi-coverage.py apply to
# themselves: finding zero of something is an error, not a pass.
REQUIRE_INTEGRATION = (
    os.environ.get("MERLON_REQUIRE_POSTGRES_OPERATIONS_INTEGRATION") == "1"
)


def postgres_image() -> str:
    compose = (ROOT / "docker-compose.yml").read_text(encoding="utf-8")
    match = re.search(r"(?m)^\s*image:\s*(postgres:[^\s]+)\s*$", compose)
    if match is None:
        raise RuntimeError("docker-compose.yml has no pinned PostgreSQL image")
    return match.group(1)


@unittest.skipUnless(
    RUN_INTEGRATION or REQUIRE_INTEGRATION,
    "set MERLON_RUN_POSTGRES_OPERATIONS_INTEGRATION=1 to run Docker tests",
)
class PostgresOperationsIntegrationTest(unittest.TestCase):
    container: str

    @classmethod
    def setUpClass(cls) -> None:
        if shutil.which("docker") is None:
            if REQUIRE_INTEGRATION:
                raise RuntimeError(
                    "MERLON_REQUIRE_POSTGRES_OPERATIONS_INTEGRATION=1 but docker "
                    "is not installed; these tests must run rather than skip"
                )
            raise unittest.SkipTest("docker is not installed")
        cls.container = f"merlon-operations-test-{uuid.uuid4().hex[:12]}"
        result = subprocess.run(
            [
                "docker",
                "run",
                "--detach",
                "--name",
                cls.container,
                "--env",
                "POSTGRES_PASSWORD=operations-test-password",
                postgres_image(),
            ],
            text=True,
            capture_output=True,
            check=False,
        )
        if result.returncode != 0:
            raise RuntimeError(result.stderr)
        try:
            for _ in range(60):
                ready = subprocess.run(
                    [
                        "docker",
                        "exec",
                        cls.container,
                        "pg_isready",
                        "--username",
                        "postgres",
                    ],
                    text=True,
                    capture_output=True,
                    check=False,
                )
                if ready.returncode == 0:
                    break
                time.sleep(0.5)
            else:
                logs = subprocess.run(
                    ["docker", "logs", cls.container],
                    text=True,
                    capture_output=True,
                    check=False,
                )
                raise RuntimeError(f"PostgreSQL did not become ready:\n{logs.stderr}")
            subprocess.run(
                [
                    "docker",
                    "exec",
                    cls.container,
                    "mkdir",
                    "-p",
                    "/repo/scripts",
                    "/repo/docs/operations",
                ],
                text=True,
                capture_output=True,
                check=True,
            )
            for source, destination in (
                (ROOT / "scripts" / "backup.sh", "/repo/scripts/backup.sh"),
                (ROOT / "scripts" / "restore.sh", "/repo/scripts/restore.sh"),
                (
                    ROOT / "docs" / "operations" / "audit-hardening.sql",
                    "/repo/docs/operations/audit-hardening.sql",
                ),
            ):
                subprocess.run(
                    [
                        "docker",
                        "cp",
                        str(source),
                        f"{cls.container}:{destination}",
                    ],
                    text=True,
                    capture_output=True,
                    check=True,
                )
        except BaseException:
            subprocess.run(
                ["docker", "rm", "--force", cls.container],
                text=True,
                capture_output=True,
                check=False,
            )
            raise

    @classmethod
    def tearDownClass(cls) -> None:
        subprocess.run(
            ["docker", "rm", "--force", cls.container],
            text=True,
            capture_output=True,
            check=False,
        )

    def docker_exec(
        self,
        args: list[str],
        *,
        input_text: str | None = None,
        env: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        command = ["docker", "exec"]
        if input_text is not None:
            command.append("--interactive")
        for name, value in (env or {}).items():
            command.extend(["--env", f"{name}={value}"])
        command.extend([self.container, *args])
        return subprocess.run(
            command,
            input=input_text,
            text=True,
            capture_output=True,
            check=False,
        )

    def psql(
        self,
        database: str,
        sql: str,
        *,
        variables: dict[str, str] | None = None,
        dsn: str | None = None,
    ) -> subprocess.CompletedProcess[str]:
        args = [
            "psql",
            "--no-psqlrc",
            "--set",
            "ON_ERROR_STOP=1",
            "--tuples-only",
            "--no-align",
        ]
        for name, value in (variables or {}).items():
            args.extend(["--set", f"{name}={value}"])
        if dsn is None:
            args.extend(["--username", "postgres", "--dbname", database])
        else:
            args.extend(["--dbname", dsn])
        return self.docker_exec(args, input_text=f"{sql}\n")

    def require_ok(
        self, result: subprocess.CompletedProcess[str], operation: str
    ) -> subprocess.CompletedProcess[str]:
        if result.returncode != 0:
            self.fail(
                f"{operation} failed ({result.returncode})\n"
                f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
            )
        return result

    def create_database(self, prefix: str) -> str:
        name = f"{prefix}_{uuid.uuid4().hex[:12]}"
        self.require_ok(
            self.psql("postgres", f'CREATE DATABASE "{name}"'),
            f"create database {name}",
        )
        return name

    def test_backup_script_uses_read_only_role_and_real_pg_dump(self) -> None:
        database = self.create_database("merlon_backup")
        suffix = uuid.uuid4().hex[:12]
        role = f"merlon_backup_{suffix}"
        password = f"Backup_{suffix}"
        role_ident = f'"{role}"'
        self.require_ok(
            self.psql(
                "postgres",
                f"CREATE ROLE {role_ident} LOGIN NOINHERIT NOSUPERUSER "
                f"NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS "
                f"PASSWORD '{password}'",
            ),
            "create backup role",
        )
        self.require_ok(
            self.psql(
                database,
                """
                CREATE TABLE customers (id bigint PRIMARY KEY, value text);
                INSERT INTO customers VALUES (1, 'sentinel');
                CREATE TABLE schema_migrations (version text PRIMARY KEY);
                INSERT INTO schema_migrations VALUES ('001');
                CREATE SEQUENCE restored_sequence START WITH 40;
                SELECT nextval('restored_sequence');
                """,
            ),
            "create backup fixtures",
        )
        self.require_ok(
            self.psql(
                database,
                f"""
                GRANT CONNECT ON DATABASE "{database}" TO {role_ident};
                REVOKE CREATE ON SCHEMA public FROM PUBLIC;
                REVOKE ALL PRIVILEGES ON SCHEMA public FROM {role_ident};
                GRANT USAGE ON SCHEMA public TO {role_ident};
                REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM {role_ident};
                GRANT SELECT ON ALL TABLES IN SCHEMA public TO {role_ident};
                REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM {role_ident};
                GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO {role_ident};
                ALTER DEFAULT PRIVILEGES FOR ROLE postgres
                  REVOKE ALL PRIVILEGES ON SCHEMAS FROM {role_ident};
                ALTER DEFAULT PRIVILEGES FOR ROLE postgres
                  GRANT USAGE ON SCHEMAS TO {role_ident};
                ALTER DEFAULT PRIVILEGES FOR ROLE postgres
                  REVOKE ALL PRIVILEGES ON TABLES FROM {role_ident};
                ALTER DEFAULT PRIVILEGES FOR ROLE postgres
                  GRANT SELECT ON TABLES TO {role_ident};
                ALTER DEFAULT PRIVILEGES FOR ROLE postgres
                  REVOKE ALL PRIVILEGES ON SEQUENCES FROM {role_ident};
                ALTER DEFAULT PRIVILEGES FOR ROLE postgres
                  GRANT SELECT ON SEQUENCES TO {role_ident};
                CREATE TABLE future_table (id bigint);
                INSERT INTO future_table VALUES (1);
                CREATE SEQUENCE future_sequence START WITH 9;
                """,
            ),
            "normalize backup role privileges",
        )

        backup_dsn = (
            f"postgresql://{role}:{password}@127.0.0.1:5432/"
            f"{database}?sslmode=disable"
        )
        output_dir = f"/tmp/merlon-backup-{suffix}"
        backup = self.docker_exec(
            ["bash", "/repo/scripts/backup.sh", output_dir],
            env={
                "MERLON_BACKUP_DATABASE_URL": backup_dsn,
                "MERLON_ENCRYPTION_KEY_RING": "v1:integration-test-key",
            },
        )
        self.require_ok(backup, "run scripts/backup.sh with real pg_dump")
        output_files = self.require_ok(
            self.docker_exec(
                [
                    "find",
                    output_dir,
                    "-maxdepth",
                    "1",
                    "-type",
                    "f",
                ]
            ),
            "find backup artifacts",
        ).stdout.splitlines()
        self.assertEqual(len(output_files), 3, output_files)
        output_dir_mode = self.require_ok(
            self.docker_exec(["stat", "-c", "%a", output_dir]),
            "inspect backup directory mode",
        ).stdout.strip()
        self.assertEqual(output_dir_mode, "700")
        for output_file in output_files:
            mode = self.require_ok(
                self.docker_exec(["stat", "-c", "%a", output_file]),
                f"inspect mode for {output_file}",
            ).stdout.strip()
            self.assertEqual(int(mode, 8) & 0o077, 0, f"{mode} {output_file}")

        dump = next(
            (
                output_file
                for output_file in output_files
                if Path(output_file).name.startswith("merlon-db-")
                and output_file.endswith(".dump")
            ),
            "",
        )
        self.assertTrue(dump, backup.stdout)
        archive_list = self.require_ok(
            self.docker_exec(["pg_restore", "--list", dump]),
            "list real backup archive",
        ).stdout
        for object_name in (
            "customers",
            "schema_migrations",
            "future_table",
            "future_sequence",
        ):
            self.assertIn(object_name, archive_list)

        readable = self.require_ok(
            self.psql(
                database,
                "SELECT (SELECT count(*) FROM future_table), "
                "(SELECT last_value FROM future_sequence)",
                dsn=backup_dsn,
            ),
            "read future table and sequence as backup role",
        ).stdout.strip()
        self.assertEqual(readable, "1|9")

        privilege_state = self.require_ok(
            self.psql(
                database,
                f"""
                SELECT
                  has_schema_privilege('{role}', 'public', 'CREATE'),
                  has_table_privilege('{role}', 'public.customers', 'INSERT'),
                  has_table_privilege('{role}', 'public.customers', 'UPDATE'),
                  has_table_privilege('{role}', 'public.customers', 'DELETE'),
                  has_table_privilege('{role}', 'public.customers', 'TRUNCATE'),
                  has_table_privilege('{role}', 'public.customers', 'REFERENCES'),
                  has_table_privilege('{role}', 'public.customers', 'TRIGGER'),
                  has_table_privilege('{role}', 'public.customers', 'MAINTAIN'),
                  has_sequence_privilege('{role}', 'public.future_sequence', 'USAGE'),
                  has_sequence_privilege('{role}', 'public.future_sequence', 'UPDATE')
                """,
            ),
            "inspect backup role forbidden privileges",
        ).stdout.strip()
        self.assertEqual(privilege_state, "f|f|f|f|f|f|f|f|f|f")

        for sql in (
            "INSERT INTO customers VALUES (2, 'forbidden')",
            "CREATE TABLE forbidden_backup_ddl (id bigint)",
            "SELECT nextval('future_sequence')",
            "SELECT setval('future_sequence', 100)",
        ):
            denied = self.psql(database, sql, dsn=backup_dsn)
            self.assertNotEqual(denied.returncode, 0, sql)

        other_owner = f"merlon_unreadable_{suffix}"
        self.require_ok(
            self.psql(
                database,
                f"""
                CREATE ROLE "{other_owner}";
                CREATE SCHEMA unreadable AUTHORIZATION "{other_owner}";
                SET ROLE "{other_owner}";
                CREATE TABLE unreadable.secret (value text);
                INSERT INTO unreadable.secret VALUES ('must-not-leak');
                RESET ROLE;
                """,
            ),
            "create a table the backup role cannot dump",
        )
        failed_output_dir = f"{output_dir}-pg-dump-failure"
        failed = self.docker_exec(
            ["bash", "/repo/scripts/backup.sh", failed_output_dir],
            env={
                "MERLON_BACKUP_DATABASE_URL": backup_dsn,
                "MERLON_ENCRYPTION_KEY_RING": "v1:integration-test-key",
            },
        )
        self.assertNotEqual(failed.returncode, 0)
        leftovers = self.require_ok(
            self.docker_exec(
                [
                    "find",
                    failed_output_dir,
                    "-mindepth",
                    "1",
                    "-maxdepth",
                    "1",
                    "-print",
                ]
            ),
            "inspect failed-backup output directory",
        ).stdout.strip()
        self.assertEqual(leftovers, "", failed.stdout + failed.stderr)

        self.require_ok(
            self.psql(database, "SELECT lo_create(0)"),
            "create unsupported large object",
        )
        refused = self.docker_exec(
            ["bash", "/repo/scripts/backup.sh", f"{output_dir}-large-object"],
            env={
                "MERLON_BACKUP_DATABASE_URL": backup_dsn,
                "MERLON_ENCRYPTION_KEY_RING": "v1:integration-test-key",
            },
        )
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn("large objects are not supported", refused.stderr)

    def provision_managed_target(self, label: str) -> tuple[str, str]:
        """Create a fresh database whose public schema a migration role manages.

        restore.sh refuses a target the restore role does not manage, so this
        is the minimum setup any restore test needs before it can exercise
        anything past that check.
        """
        database = self.create_database(label)
        suffix = uuid.uuid4().hex[:12]
        role = f"merlon_migrate_{suffix}"
        password = f"Migrate_{suffix}"
        self.require_ok(
            self.psql(
                "postgres",
                f"CREATE ROLE \"{role}\" LOGIN PASSWORD '{password}'",
            ),
            "create migration role",
        )
        self.require_ok(
            self.psql(
                "postgres",
                f'GRANT CREATE ON DATABASE "{database}" TO "{role}"',
            ),
            "temporarily allow schema ownership transfer",
        )
        self.require_ok(
            self.psql(database, f'ALTER SCHEMA public OWNER TO "{role}"'),
            "transfer public schema ownership to migration role",
        )
        self.require_ok(
            self.psql(
                "postgres",
                f'REVOKE CREATE ON DATABASE "{database}" FROM "{role}"',
            ),
            "remove temporary database-level create privilege",
        )
        dsn = (
            f"postgresql://{role}:{password}@127.0.0.1:5432/"
            f"{database}?sslmode=disable"
        )
        return database, dsn

    def assert_target_untouched(self, database: str, operation: str) -> None:
        untouched = self.require_ok(
            self.psql(database, "SELECT to_regclass('public.customers') IS NULL"),
            operation,
        ).stdout.strip()
        self.assertEqual(untouched, "t")

    def test_restore_refuses_unusable_serving_role_before_pg_restore(self) -> None:
        # audit-hardening.sql runs after pg_restore and hard-fails on serving-role
        # preconditions that have nothing to do with the dump. Without this
        # preflight the failure lands on a database that is already restored,
        # has no serving-role grants, and never printed the post-restore
        # checklist -- the worst possible moment to discover it.
        source = self.create_database("merlon_role_source")
        self.require_ok(
            self.psql(
                source,
                """
                CREATE TABLE customers (id bigint PRIMARY KEY, value text);
                INSERT INTO customers VALUES (7, 'preflight-sentinel');
                """,
            ),
            "create source schema for the serving-role preflight",
        )
        suffix = uuid.uuid4().hex[:12]
        dump = f"/tmp/merlon-db-{suffix}.dump"
        self.require_ok(
            self.docker_exec(
                [
                    "pg_dump",
                    "--username",
                    "postgres",
                    "--format=custom",
                    "--no-owner",
                    "--no-privileges",
                    f"--file={dump}",
                    source,
                ]
            ),
            "create archive for the serving-role preflight",
        )

        database, dsn = self.provision_managed_target("merlon_role_target")
        absent = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": dsn,
                "MERLON_APP_ROLE": f"merlon_absent_{suffix}",
            },
        )
        self.assertNotEqual(absent.returncode, 0)
        self.assertIn("does not exist", absent.stderr)
        self.assert_target_untouched(
            database, "verify the missing-role rejection preceded pg_restore"
        )

        app_role = f"merlon_app_{suffix}"
        self.require_ok(
            self.psql("postgres", f'CREATE ROLE "{app_role}" LOGIN'),
            "create serving role holding a forbidden privilege",
        )
        self.require_ok(
            self.psql(
                "postgres",
                f'GRANT CREATE ON DATABASE "{database}" TO "{app_role}"',
            ),
            "grant the forbidden database CREATE",
        )
        forbidden = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": dsn,
                "MERLON_APP_ROLE": app_role,
            },
        )
        self.assertNotEqual(forbidden.returncode, 0)
        self.assertIn("forbidden CREATE", forbidden.stderr)
        self.assert_target_untouched(
            database, "verify the database-CREATE rejection preceded pg_restore"
        )

    def test_restore_verifies_the_manifest_checksum_and_finds_siblings(self) -> None:
        # The backup lives under a directory whose own name starts with
        # "merlon-db-". Deriving the manifest and key-ring names by substituting
        # on the whole path rewrites that directory and leaves the filename
        # alone, so this layout is what tells a correct derivation from one that
        # reports a key ring missing while it sits right next to the dump.
        source = self.create_database("merlon_manifest_source")
        self.require_ok(
            self.psql(
                source,
                """
                CREATE TABLE customers (id bigint PRIMARY KEY, value text);
                INSERT INTO customers VALUES (11, 'manifest-sentinel');
                """,
            ),
            "create source schema for the manifest check",
        )
        suffix = uuid.uuid4().hex[:12]
        out_dir = f"/tmp/merlon-db-archive-{suffix}"
        self.require_ok(
            self.docker_exec(
                ["bash", "/repo/scripts/backup.sh", out_dir],
                env={
                    "MERLON_BACKUP_DATABASE_URL": (
                        "postgresql://postgres:operations-test-password@"
                        f"127.0.0.1:5432/{source}?sslmode=disable"
                    ),
                    "MERLON_ENCRYPTION_KEY_RING": "v1:manifest-test-key",
                },
            ),
            "produce a real backup set for the manifest check",
        )
        artifacts = self.require_ok(
            self.docker_exec(["find", out_dir, "-maxdepth", "1", "-type", "f"]),
            "find backup artifacts",
        ).stdout.split()
        dump = next(
            path
            for path in artifacts
            if Path(path).name.startswith("merlon-db-") and path.endswith(".dump")
        )

        database, dsn = self.provision_managed_target("merlon_manifest_target")
        app_role = f"merlon_app_{suffix}"
        self.require_ok(
            self.psql("postgres", f'CREATE ROLE "{app_role}" LOGIN'),
            "create serving role for the manifest check",
        )
        restored = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": dsn,
                "MERLON_APP_ROLE": app_role,
            },
        )
        self.require_ok(restored, "restore a verified backup set")
        self.assertIn("Checksum matches.", restored.stdout)
        self.assertIn("Matching key ring found", restored.stdout)
        self.assertNotIn("no matching key ring", restored.stderr)
        value = self.require_ok(
            self.psql(database, "SELECT value FROM customers WHERE id = 11"),
            "verify the restored row",
        ).stdout.strip()
        self.assertEqual(value, "manifest-sentinel")

        # pg_restore accepts any structurally valid custom archive, so a dump
        # that no longer matches its manifest has to be caught here or not at all.
        self.require_ok(
            self.docker_exec(["bash", "-c", f"printf 'corrupt' >> {dump}"]),
            "corrupt the dump after the manifest was written",
        )
        corrupt_target, corrupt_dsn = self.provision_managed_target(
            "merlon_manifest_corrupt"
        )
        refused = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": corrupt_dsn,
                "MERLON_APP_ROLE": app_role,
            },
        )
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn("checksum mismatch", refused.stderr)
        self.assert_target_untouched(
            corrupt_target, "verify the checksum rejection preceded pg_restore"
        )

    def test_restore_rejects_nonfresh_target_and_restores_fresh_target(self) -> None:
        source = self.create_database("merlon_source")
        nonfresh = self.create_database("merlon_nonfresh")
        fresh = self.create_database("merlon_fresh")
        suffix = uuid.uuid4().hex[:12]
        app_role = f"merlon_app_{suffix}"
        app_password = f"App_{suffix}"
        migration_role = f"merlon_migrate_{suffix}"
        migration_password = f"Migrate_{suffix}"
        self.require_ok(
            self.psql(
                "postgres",
                f"""
                CREATE ROLE "{app_role}" LOGIN PASSWORD '{app_password}';
                CREATE ROLE "{migration_role}" LOGIN PASSWORD '{migration_password}';
                """,
            ),
            "create restore application and migration roles",
        )
        self.require_ok(
            self.psql(
                source,
                """
                CREATE TABLE customers (id bigint PRIMARY KEY, value text);
                INSERT INTO customers VALUES (41, 'restored-sentinel');
                """,
            ),
            "create source archive schema",
        )
        dump = f"/tmp/merlon-source-{suffix}.dump"
        self.require_ok(
            self.docker_exec(
                [
                    "pg_dump",
                    "--username",
                    "postgres",
                    "--format=custom",
                    "--no-owner",
                    "--no-privileges",
                    f"--file={dump}",
                    source,
                ]
            ),
            "create real source archive",
        )
        self.require_ok(
            self.psql(nonfresh, "CREATE TABLE seed_state (id bigint)"),
            "create newer target object",
        )
        refused = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": (
                    "postgresql://postgres:operations-test-password@"
                    f"127.0.0.1:5432/{nonfresh}?sslmode=disable"
                ),
                "MERLON_APP_ROLE": app_role,
            },
        )
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn("fresh target database is required", refused.stderr)
        still_present = self.require_ok(
            self.psql(nonfresh, "SELECT to_regclass('public.seed_state') IS NOT NULL"),
            "verify rejected target is unchanged",
        ).stdout.strip()
        self.assertEqual(still_present, "t")

        self.require_ok(
            self.psql(
                "postgres",
                f"""
                REVOKE CONNECT ON DATABASE "{fresh}" FROM PUBLIC;
                GRANT CONNECT ON DATABASE "{fresh}" TO "{migration_role}";
                GRANT CONNECT ON DATABASE "{fresh}" TO "{app_role}";
                """,
            ),
            "provision direct fresh-target connections",
        )
        migration_dsn = (
            f"postgresql://{migration_role}:{migration_password}@"
            f"127.0.0.1:5432/{fresh}?sslmode=disable"
        )
        unmanaged = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": migration_dsn,
                "MERLON_APP_ROLE": app_role,
            },
        )
        self.assertNotEqual(unmanaged.returncode, 0)
        self.assertIn(
            "must own or manage the public schema",
            unmanaged.stderr,
        )
        still_fresh = self.require_ok(
            self.psql(fresh, "SELECT to_regclass('public.customers') IS NULL"),
            "verify schema-access rejection happened before restore",
        ).stdout.strip()
        self.assertEqual(still_fresh, "t")

        self.require_ok(
            self.psql(
                "postgres",
                f'GRANT CREATE ON DATABASE "{fresh}" TO "{migration_role}"',
            ),
            "temporarily allow schema ownership transfer",
        )
        self.require_ok(
            self.psql(
                fresh,
                f'ALTER SCHEMA public OWNER TO "{migration_role}"',
            ),
            "transfer public schema ownership to migration role",
        )
        self.require_ok(
            self.psql(
                "postgres",
                f'REVOKE CREATE ON DATABASE "{fresh}" FROM "{migration_role}"',
            ),
            "remove temporary database-level create privilege",
        )
        restored = self.docker_exec(
            ["bash", "/repo/scripts/restore.sh", dump],
            input_text="restore\n",
            env={
                "MERLON_MIGRATION_DATABASE_URL": migration_dsn,
                "MERLON_APP_ROLE": app_role,
            },
        )
        self.require_ok(restored, "restore real archive into fresh target")
        restored_state = self.require_ok(
            self.psql(
                fresh,
                "SELECT value, to_regclass('public.seed_state') IS NULL "
                "FROM customers WHERE id = 41",
            ),
            "verify restored data and absent newer table",
        ).stdout.strip()
        self.assertEqual(restored_state, "restored-sentinel|t")
        app_dsn = (
            f"postgresql://{app_role}:{app_password}@127.0.0.1:5432/"
            f"{fresh}?sslmode=disable"
        )
        app_read = self.require_ok(
            self.psql(fresh, "SELECT value FROM customers WHERE id = 41", dsn=app_dsn),
            "connect and read as application role with PUBLIC CONNECT revoked",
        ).stdout.strip()
        self.assertEqual(app_read, "restored-sentinel")

    def test_real_psql_path_quotes_metacharacter_role_name(self) -> None:
        database = self.create_database("merlon_role")
        role = "x';CREATE ROLE injected_marker;--\n:\"\\z"
        suffix = uuid.uuid4().hex[:12]
        migration_role = f"merlon_migrate_{suffix}"
        migration_password = f"Migrate_{suffix}"
        self.require_ok(
            self.psql(
                "postgres",
                "SELECT format('CREATE ROLE %I', :'ROLE_NAME') \\gexec",
                variables={"ROLE_NAME": role},
            ),
            "create metacharacter role through psql variables",
        )
        self.require_ok(
            self.psql(
                "postgres",
                f'CREATE ROLE "{migration_role}" LOGIN '
                f"PASSWORD '{migration_password}'",
            ),
            "create non-database-owner migration role",
        )
        self.require_ok(
            self.psql(
                database,
                f'ALTER SCHEMA public OWNER TO "{migration_role}"',
            ),
            "assign public schema ownership to migration role",
        )
        migration_dsn = (
            f"postgresql://{migration_role}:{migration_password}@"
            f"127.0.0.1:5432/{database}?sslmode=disable"
        )
        self.require_ok(
            self.psql(
                database,
                """
                CREATE TABLE customers (id bigint);
                CREATE TABLE audit_logs (
                  id bigserial PRIMARY KEY,
                  action text,
                  resource_type text
                );
                CREATE TABLE rule_activation_events (id bigint);
                CREATE TABLE schema_migrations (version text);
                """,
                dsn=migration_dsn,
            ),
            "create role-grant fixtures",
        )
        applied = self.docker_exec(
            [
                "psql",
                "--dbname",
                migration_dsn,
                "--no-psqlrc",
                "--set",
                "ON_ERROR_STOP=1",
                "--set",
                f"MERLON_APP_ROLE={role}",
                "--file",
                "/repo/docs/operations/audit-hardening.sql",
            ]
        )
        self.require_ok(applied, "apply grant procedure through real psql")
        state = self.require_ok(
            self.psql(
                database,
                """
                SELECT
                  has_table_privilege(:'ROLE_NAME', 'public.customers', 'SELECT'),
                  has_table_privilege(:'ROLE_NAME', 'public.audit_logs', 'UPDATE'),
                  has_table_privilege(:'ROLE_NAME', 'public.customers', 'MAINTAIN'),
                  has_sequence_privilege(
                    :'ROLE_NAME',
                    'public.audit_logs_id_seq',
                    'USAGE'
                  ),
                  EXISTS (
                    SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = 'injected_marker'
                  )
                """,
                variables={"ROLE_NAME": role},
            ),
            "inspect exact metacharacter role grants",
        ).stdout.strip()
        self.assertEqual(state, "t|f|f|t|f")


class IntegrationSuiteFloorTest(unittest.TestCase):
    """Runs unconditionally, including without Docker.

    Everything above is opt-in, which means this module's exit status alone
    cannot tell "all the integration tests passed" from "there were none".
    This class is the floor: it cannot be skipped, so deleting or renaming the
    integration tests fails here instead of quietly reporting success.
    """

    def test_the_integration_suite_still_has_tests(self) -> None:
        names = unittest.defaultTestLoader.getTestCaseNames(
            PostgresOperationsIntegrationTest
        )
        self.assertGreaterEqual(len(names), 5, names)

    def test_requiring_integration_also_enables_it(self) -> None:
        # REQUIRE implies RUN: a CI job that sets only the require flag must
        # still execute the suite rather than skip it and pass the floor.
        if not REQUIRE_INTEGRATION:
            self.skipTest("MERLON_REQUIRE_POSTGRES_OPERATIONS_INTEGRATION is not set")
        self.assertFalse(
            getattr(PostgresOperationsIntegrationTest, "__unittest_skip__", False),
            "integration tests are marked skipped while they are required",
        )


if __name__ == "__main__":
    unittest.main()
