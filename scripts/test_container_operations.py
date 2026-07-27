#!/usr/bin/env python3
"""Regression tests for container probes and backup/restore entry points."""

from __future__ import annotations

import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
HEALTHCHECK = ROOT / "api" / "healthcheck.sh"
BACKUP = ROOT / "scripts" / "backup.sh"
RESTORE = ROOT / "scripts" / "restore.sh"
ROLE_GRANTS = ROOT / "docs" / "operations" / "audit-hardening.sql"


def write_executable(path: Path, contents: str) -> None:
    path.write_text(contents, encoding="utf-8")
    path.chmod(path.stat().st_mode | stat.S_IXUSR)


class HealthcheckTest(unittest.TestCase):
    def run_probe(
        self,
        *,
        mode: str = "all",
        http_addr: str = ":8080",
        worker_addr: str = ":8081",
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            args_file = tmp_path / "wget-args"
            write_executable(
                tmp_path / "wget",
                "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_ARGS_FILE\"\n",
            )
            env = {
                **os.environ,
                "PATH": f"{tmp_path}:{os.environ['PATH']}",
                "FAKE_ARGS_FILE": str(args_file),
                "MERLON_MODE": mode,
                "MERLON_HTTP_ADDR": http_addr,
                "MERLON_WORKER_HTTP_ADDR": worker_addr,
            }
            result = subprocess.run(
                ["/bin/sh", str(HEALTHCHECK)],
                env=env,
                text=True,
                capture_output=True,
                check=False,
            )
            result.wget_args = (
                args_file.read_text(encoding="utf-8").splitlines()
                if args_file.exists()
                else []
            )
            return result

    def test_worker_probes_worker_listener(self):
        result = self.run_probe(
            mode="worker", http_addr=":8080", worker_addr=":18081"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.wget_args,
            ["--spider", "-q", "http://127.0.0.1:18081/healthz/live"],
        )

    def test_api_and_all_probe_http_listener(self):
        for mode in ("api", "all"):
            with self.subTest(mode=mode):
                result = self.run_probe(
                    mode=mode, http_addr=":18080", worker_addr=":18081"
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(
                    result.wget_args[-1],
                    "http://127.0.0.1:18080/healthz/live",
                )

    def test_listener_hosts_are_normalized_to_valid_urls(self):
        cases = {
            "0.0.0.0:8080": "http://127.0.0.1:8080/healthz/live",
            "localhost:9090": "http://localhost:9090/healthz/live",
            "127.0.0.1:9091": "http://127.0.0.1:9091/healthz/live",
            "[::]:8081": "http://[::1]:8081/healthz/live",
            "[::1]:8082": "http://[::1]:8082/healthz/live",
        }
        for listener, expected in cases.items():
            with self.subTest(listener=listener):
                result = self.run_probe(http_addr=listener)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.wget_args[-1], expected)

    def test_invalid_listener_fails_without_invoking_wget(self):
        for listener in ("8080", "localhost:not-a-port", "localhost:8080/path"):
            with self.subTest(listener=listener):
                result = self.run_probe(http_addr=listener)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.wget_args, [])


class BackupTest(unittest.TestCase):
    def test_backup_help_contains_only_documented_usage(self):
        result = subprocess.run(
            ["bash", str(BACKUP), "--help"],
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.startswith("Take a Merlon backup:"))
        self.assertNotIn("umask 077", result.stdout)

    def run_backup(
        self,
        *,
        backup_url: str | None,
        migration_url: str | None,
        app_url: str | None,
        large_object_count: int = 0,
        pg_dump_exit: int = 0,
    ) -> tuple[subprocess.CompletedProcess[str], list[str]]:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            args_file = tmp_path / "pg-dump-args"
            write_executable(
                tmp_path / "pg_dump",
                "#!/bin/sh\n"
                "printf '%s\\n' \"$@\" > \"$FAKE_ARGS_FILE\"\n"
                "for arg in \"$@\"; do\n"
                "  case \"$arg\" in --file=*) printf partial > \"${arg#--file=}\" ;; esac\n"
                "done\n"
                "exit \"$FAKE_PG_DUMP_EXIT\"\n",
            )
            write_executable(
                tmp_path / "psql",
                "#!/bin/sh\nprintf '%s\\n' \"$FAKE_LARGE_OBJECT_COUNT\"\n",
            )
            env = {
                **os.environ,
                "PATH": f"{tmp_path}:{os.environ['PATH']}",
                "FAKE_ARGS_FILE": str(args_file),
                "FAKE_LARGE_OBJECT_COUNT": str(large_object_count),
                "FAKE_PG_DUMP_EXIT": str(pg_dump_exit),
                "MERLON_ENCRYPTION_KEY_RING": "v1:test-backup-key",
            }
            env.pop("MERLON_DATABASE_URL", None)
            env.pop("MERLON_MIGRATION_DATABASE_URL", None)
            env.pop("MERLON_BACKUP_DATABASE_URL", None)
            if backup_url is not None:
                env["MERLON_BACKUP_DATABASE_URL"] = backup_url
            if migration_url is not None:
                env["MERLON_MIGRATION_DATABASE_URL"] = migration_url
            if app_url is not None:
                env["MERLON_DATABASE_URL"] = app_url
            result = subprocess.run(
                ["bash", str(BACKUP), str(tmp_path / "output")],
                env=env,
                text=True,
                capture_output=True,
                check=False,
            )
            args = (
                args_file.read_text(encoding="utf-8").splitlines()
                if args_file.exists()
                else []
            )
            output_dir = tmp_path / "output"
            result.output_files = (
                sorted(path.name for path in output_dir.iterdir())
                if output_dir.exists()
                else []
            )
            result.output_modes = (
                {
                    path.name: stat.S_IMODE(path.stat().st_mode)
                    for path in output_dir.iterdir()
                }
                if output_dir.exists()
                else {}
            )
            result.output_dir_mode = (
                stat.S_IMODE(output_dir.stat().st_mode)
                if output_dir.exists()
                else None
            )
            return result, args

    def test_backup_requires_dedicated_read_only_connection(self):
        result, args = self.run_backup(
            backup_url=None,
            migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
            app_url="postgres://merlon_app:app-secret@db/merlon",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("MERLON_BACKUP_DATABASE_URL is required", result.stderr)
        self.assertEqual(args, [])

    def test_backup_dumps_with_read_only_role_not_privileged_roles(self):
        backup_url = "postgres://merlon_backup:backup-secret@db/merlon"
        migration_url = "postgres://merlon_migrate:owner-secret@db/merlon"
        app_url = "postgres://merlon_app:app-secret@db/merlon"
        result, args = self.run_backup(
            backup_url=backup_url,
            migration_url=migration_url,
            app_url=app_url,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(backup_url, args)
        self.assertNotIn(migration_url, args)
        self.assertNotIn(app_url, args)
        self.assertIn("--no-owner", args)
        self.assertIn("--no-privileges", args)
        self.assertEqual(result.output_dir_mode, 0o700)
        self.assertTrue(result.output_modes)
        for name, mode in result.output_modes.items():
            with self.subTest(name=name):
                self.assertEqual(mode & 0o077, 0, oct(mode))

    def test_backup_removes_temporary_dump_after_pg_dump_failure(self):
        result, _ = self.run_backup(
            backup_url="postgres://merlon_backup:backup-secret@db/merlon",
            migration_url=None,
            app_url=None,
            pg_dump_exit=29,
        )
        self.assertEqual(result.returncode, 29)
        self.assertEqual(result.output_files, [])

    def test_backup_rejects_large_objects_before_pg_dump(self):
        result, args = self.run_backup(
            backup_url="postgres://merlon_backup:backup-secret@db/merlon",
            migration_url=None,
            app_url=None,
            large_object_count=1,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("large objects are not supported", result.stderr)
        self.assertEqual(args, [])


class RestoreTest(unittest.TestCase):
    def run_restore(
        self,
        *,
        migration_url: str | None,
        app_url: str | None,
        pg_restore_exit: int = 0,
        fresh_target: bool = True,
        schema_access: bool = True,
        psql_grants_exit: int = 0,
        app_role: str | None = None,
        app_role_ready: str = "ok",
    ) -> tuple[subprocess.CompletedProcess[str], list[str]]:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            dump_file = tmp_path / "merlon-db-20260726T090000Z.dump"
            dump_file.touch()
            args_file = tmp_path / "pg-restore-args"
            psql_args_file = tmp_path / "psql-args"
            psql_grants_args_file = tmp_path / "psql-grants-args"
            events_file = tmp_path / "events"
            write_executable(
                tmp_path / "pg_restore",
                "#!/bin/sh\n"
                "printf '%s\\n' \"$@\" > \"$FAKE_ARGS_FILE\"\n"
                "printf '%s\\n' pg_restore >> \"$FAKE_EVENTS_FILE\"\n"
                "exit \"$FAKE_PG_RESTORE_EXIT\"\n",
            )
            write_executable(
                tmp_path / "psql",
                "#!/bin/sh\n"
                "case \" $* \" in\n"
                "  *'SELECT current_user'*)\n"
                "    printf '%s\\n' \"$@\" > \"$FAKE_PSQL_ARGS_FILE\"\n"
                "    printf '%s\\n' psql-identity >> \"$FAKE_EVENTS_FILE\"\n"
                "    printf '%s\\n' 'merlon_migrate@db:5432/merlon'\n"
                "    ;;\n"
                "  *'MERLON_FRESH_TARGET'*)\n"
                "    printf '%s\\n' psql-freshness >> \"$FAKE_EVENTS_FILE\"\n"
                "    printf '%s\\n' \"$FAKE_FRESH_TARGET\"\n"
                "    ;;\n"
                "  *'MERLON_SCHEMA_ACCESS'*)\n"
                "    printf '%s\\n' psql-schema-access >> \"$FAKE_EVENTS_FILE\"\n"
                "    printf '%s\\n' \"$FAKE_SCHEMA_ACCESS\"\n"
                "    ;;\n"
                # The serving-role preflight sends its SQL on stdin (psql only
                # interpolates :'app_role' from a file or stdin), so it is
                # matched on the --set that carries the role name rather than
                # on the query text like the checks above.
                "  *'--set app_role='*)\n"
                "    printf '%s\\n' psql-app-role >> \"$FAKE_EVENTS_FILE\"\n"
                "    printf '%s\\n' \"$FAKE_APP_ROLE_READY\"\n"
                "    ;;\n"
                "  *)\n"
                "    printf '%s\\n' \"$@\" > \"$FAKE_PSQL_GRANTS_ARGS_FILE\"\n"
                "    printf '%s\\n' psql-grants >> \"$FAKE_EVENTS_FILE\"\n"
                "    exit \"$FAKE_PSQL_GRANTS_EXIT\"\n"
                "    ;;\n"
                "esac\n",
            )
            env = {
                **os.environ,
                "PATH": f"{tmp_path}:{os.environ['PATH']}",
                "FAKE_ARGS_FILE": str(args_file),
                "FAKE_PSQL_ARGS_FILE": str(psql_args_file),
                "FAKE_PSQL_GRANTS_ARGS_FILE": str(psql_grants_args_file),
                "FAKE_EVENTS_FILE": str(events_file),
                "FAKE_PG_RESTORE_EXIT": str(pg_restore_exit),
                "FAKE_FRESH_TARGET": "fresh" if fresh_target else "not-fresh",
                "FAKE_SCHEMA_ACCESS": "managed" if schema_access else "unmanaged",
                "FAKE_APP_ROLE_READY": app_role_ready,
                "FAKE_PSQL_GRANTS_EXIT": str(psql_grants_exit),
            }
            env.pop("MERLON_DATABASE_URL", None)
            env.pop("MERLON_MIGRATION_DATABASE_URL", None)
            env.pop("MERLON_APP_ROLE", None)
            if migration_url is not None:
                env["MERLON_MIGRATION_DATABASE_URL"] = migration_url
            if app_url is not None:
                env["MERLON_DATABASE_URL"] = app_url
            if app_role is not None:
                env["MERLON_APP_ROLE"] = app_role

            result = subprocess.run(
                ["bash", str(RESTORE), str(dump_file)],
                input="restore\n",
                env=env,
                text=True,
                capture_output=True,
                check=False,
            )
            result.psql_grants_args = (
                psql_grants_args_file.read_text(encoding="utf-8").splitlines()
                if psql_grants_args_file.exists()
                else []
            )
            result.events = (
                events_file.read_text(encoding="utf-8").splitlines()
                if events_file.exists()
                else []
            )
            args = (
                args_file.read_text(encoding="utf-8").splitlines()
                if args_file.exists()
                else []
            )
            return result, args

    def test_restore_rejects_serving_role_without_schema_owner(self):
        result, args = self.run_restore(
            migration_url=None,
            app_url="postgres://merlon_app:app-secret@db/merlon",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("MERLON_MIGRATION_DATABASE_URL is required", result.stderr)
        self.assertEqual(args, [])

    def test_restore_passes_only_schema_owner_url_to_pg_restore(self):
        migration_url = "postgres://merlon_migrate:owner-secret@db/merlon"
        app_url = "postgres://merlon_app:app-secret@db/merlon"
        result, args = self.run_restore(
            migration_url=migration_url,
            app_url=app_url,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(migration_url, args)
        self.assertNotIn(app_url, args)
        self.assertNotIn("owner-secret", result.stdout + result.stderr)
        self.assertIn("merlon_migrate@db:5432/merlon", result.stdout)
        self.assertEqual(
            result.events,
            [
                "psql-identity",
                "psql-freshness",
                "psql-schema-access",
                "psql-app-role",
                "pg_restore",
                "psql-grants",
            ],
        )
        self.assertIn("--single-transaction", args)
        self.assertIn("--exit-on-error", args)
        self.assertTrue(
            any(migration_url in arg for arg in result.psql_grants_args),
            result.psql_grants_args,
        )
        self.assertIn("MERLON_APP_ROLE=merlon_app", result.psql_grants_args)
        self.assertIn(str(ROLE_GRANTS), result.psql_grants_args)
        self.assertIn(
            "Reapply dedicated backup-role grants/defaults and all organization-specific ACLs.",
            result.stdout,
        )
        self.assertIn("6. Check readiness:", result.stdout)
        self.assertIn(
            "Point MERLON_DATABASE_URL at the fresh target",
            result.stdout,
        )
        self.assertIn("fresh target", result.stdout)

    def test_restore_never_echoes_any_supported_dsn_password_form(self):
        cases = (
            (
                "postgres://merlon_migrate:authority-secret@db/merlon",
                "authority-secret",
            ),
            (
                "postgresql://merlon_migrate@db/merlon?password=query-secret",
                "query-secret",
            ),
            (
                "host=db dbname=merlon user=merlon_migrate password=keyword-secret",
                "keyword-secret",
            ),
        )
        for migration_url, secret in cases:
            with self.subTest(migration_url=migration_url):
                result, _ = self.run_restore(
                    migration_url=migration_url,
                    app_url=None,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn(secret, result.stdout + result.stderr)
                self.assertIn("merlon_migrate@db:5432/merlon", result.stdout)

    def test_restore_propagates_pg_restore_failure(self):
        result, _ = self.run_restore(
            migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
            app_url=None,
            pg_restore_exit=23,
        )
        self.assertEqual(result.returncode, 23)
        self.assertNotIn("Restore complete", result.stdout)
        self.assertEqual(
            result.events,
            [
                "psql-identity",
                "psql-freshness",
                "psql-schema-access",
                "psql-app-role",
                "pg_restore",
            ],
        )

    def test_restore_rejects_nonempty_target_before_prompt_or_pg_restore(self):
        result, args = self.run_restore(
            migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
            app_url=None,
            fresh_target=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fresh target database is required", result.stderr)
        self.assertEqual(result.events, ["psql-identity", "psql-freshness"])
        self.assertEqual(args, [])

    def test_restore_rejects_role_without_public_schema_management(self):
        result, args = self.run_restore(
            migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
            app_url=None,
            schema_access=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must own or manage the public schema", result.stderr)
        self.assertEqual(
            result.events,
            ["psql-identity", "psql-freshness", "psql-schema-access"],
        )
        self.assertEqual(args, [])

    def test_restore_rejects_unusable_serving_role_before_pg_restore(self):
        # audit-hardening.sql raises on these after pg_restore has already run.
        # Reaching pg_restore for any of them leaves a restored database with
        # no serving-role grants and no printed recovery steps.
        cases = (
            ("missing", "does not exist"),
            ("superuser", "must not be a superuser"),
            ("database-create", "forbidden CREATE"),
            ("cannot-grant-connect", "lacks CONNECT"),
            # An unrecognized verdict must fail closed rather than fall through.
            ("", "could not verify"),
        )
        for verdict, message in cases:
            with self.subTest(verdict=verdict or "(empty)"):
                result, args = self.run_restore(
                    migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
                    app_url=None,
                    app_role_ready=verdict,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertEqual(
                    result.events,
                    [
                        "psql-identity",
                        "psql-freshness",
                        "psql-schema-access",
                        "psql-app-role",
                    ],
                )
                self.assertEqual(args, [])

    def test_restore_propagates_serving_role_grant_failure(self):
        result, _ = self.run_restore(
            migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
            app_url=None,
            psql_grants_exit=24,
        )
        self.assertEqual(result.returncode, 24)
        self.assertEqual(
            result.events,
            [
                "psql-identity",
                "psql-freshness",
                "psql-schema-access",
                "psql-app-role",
                "pg_restore",
                "psql-grants",
            ],
        )
        self.assertNotIn("Restore complete", result.stdout)

    def test_restore_passes_configured_application_role_safely(self):
        result, _ = self.run_restore(
            migration_url="postgres://merlon_migrate:owner-secret@db/merlon",
            app_url=None,
            app_role="merlon_serving",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            "MERLON_APP_ROLE=merlon_serving",
            result.psql_grants_args,
        )


class PackagingContractTest(unittest.TestCase):
    def test_directly_invoked_backup_scripts_are_executable_in_git(self):
        for script in ("scripts/backup.sh", "scripts/restore.sh"):
            with self.subTest(script=script):
                index_entry = subprocess.run(
                    ["git", "ls-files", "--stage", "--", script],
                    cwd=ROOT,
                    text=True,
                    capture_output=True,
                    check=True,
                ).stdout
                self.assertTrue(index_entry.startswith("100755 "), index_entry)

    def test_standard_compose_forwards_required_jwt_secret(self):
        compose = (ROOT / "docker-compose.yml").read_text(encoding="utf-8")
        self.assertIn(
            "MERLON_JWT_SECRET: ${MERLON_JWT_SECRET:?set MERLON_JWT_SECRET}",
            compose,
        )

    def test_image_installs_and_executes_healthcheck_helper(self):
        dockerfile = (ROOT / "api" / "Dockerfile").read_text(encoding="utf-8")
        self.assertIn(
            "COPY --chmod=0555 api/healthcheck.sh /usr/local/bin/merlon-healthcheck",
            dockerfile,
        )
        self.assertIn('CMD ["/usr/local/bin/merlon-healthcheck"]', dockerfile)


if __name__ == "__main__":
    unittest.main()
