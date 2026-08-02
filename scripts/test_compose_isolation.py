"""Regression tests for Docker Compose project and port isolation."""

import json
import os
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]


class ComposeIsolationTests(unittest.TestCase):
    """Keep local Compose stacks independently addressable and persistent."""

    def compose_config(self, project, files, **overrides):
        """Return parsed Compose config using deterministic local placeholders."""
        environment = os.environ.copy()
        environment.update(
            {
                "MERLON_POSTGRES_PASSWORD": "p00-local-only-do-not-reuse",
                "MERLON_BOOTSTRAP_TOKEN": "p00-local-only-do-not-reuse",
                "MERLON_JWT_SECRET": "p00-local-only-do-not-reuse-32-bytes-minimum",
            }
        )
        environment.pop("MERLON_API_HOST_PORT", None)
        environment.pop("MERLON_DB_HOST_PORT", None)
        environment.update({key: str(value) for key, value in overrides.items()})

        command = ["docker", "compose", "-p", project]
        for compose_file in files:
            command.extend(("-f", compose_file))
        command.extend(("config", "--format", "json"))

        try:
            result = subprocess.run(
                command,
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
                check=False,
            )
        except FileNotFoundError as error:
            self.fail(f"Docker Compose CLI is required for this test: {error}")

        if result.returncode != 0:
            self.fail(
                "docker compose config failed\n"
                f"command: {' '.join(command)}\n"
                f"stdout:\n{result.stdout}\n"
                f"stderr:\n{result.stderr}"
            )
        return json.loads(result.stdout)

    @staticmethod
    def published_ports(config, service):
        return config["services"][service].get("ports", [])

    def test_standard_projects_have_distinct_names_and_override_api_ports(self):
        stack_a = self.compose_config(
            "merlon-p00-a", ("docker-compose.yml",), MERLON_API_HOST_PORT=18050
        )
        stack_b = self.compose_config(
            "merlon-p00-b", ("docker-compose.yml",), MERLON_API_HOST_PORT=18051
        )

        self.assertEqual(stack_a["name"], "merlon-p00-a")
        self.assertEqual(stack_b["name"], "merlon-p00-b")
        self.assertNotEqual(stack_a["name"], stack_b["name"])
        self.assertEqual(
            self.published_ports(stack_a, "api"),
            [{"mode": "ingress", "target": 8080, "published": "18050", "protocol": "tcp"}],
        )
        self.assertEqual(
            self.published_ports(stack_b, "api"),
            [{"mode": "ingress", "target": 8080, "published": "18051", "protocol": "tcp"}],
        )

    def test_standard_config_does_not_publish_database_port(self):
        config = self.compose_config(
            "merlon-p00-a", ("docker-compose.yml",), MERLON_API_HOST_PORT=18050
        )

        self.assertEqual(self.published_ports(config, "db"), [])

    def test_demo_api_binds_loopback_with_overridable_port(self):
        stack_a = self.compose_config(
            "merlon-p00-a",
            ("docker-compose.demo.yml",),
            MERLON_API_HOST_PORT=18050,
        )
        stack_b = self.compose_config(
            "merlon-p00-b",
            ("docker-compose.demo.yml",),
            MERLON_API_HOST_PORT=18051,
        )

        self.assertEqual(
            self.published_ports(stack_a, "api"),
            [
                {
                    "host_ip": "127.0.0.1",
                    "mode": "ingress",
                    "target": 8080,
                    "published": "18050",
                    "protocol": "tcp",
                }
            ],
        )
        self.assertEqual(
            self.published_ports(stack_b, "api"),
            [
                {
                    "host_ip": "127.0.0.1",
                    "mode": "ingress",
                    "target": 8080,
                    "published": "18051",
                    "protocol": "tcp",
                }
            ],
        )

    def test_test_overlay_publishes_database_on_loopback(self):
        stack_a = self.compose_config(
            "merlon-p00-a",
            ("docker-compose.demo.yml", "docker-compose.test.yml"),
            MERLON_API_HOST_PORT=18050,
            MERLON_DB_HOST_PORT=15450,
        )
        stack_b = self.compose_config(
            "merlon-p00-b",
            ("docker-compose.demo.yml", "docker-compose.test.yml"),
            MERLON_API_HOST_PORT=18051,
            MERLON_DB_HOST_PORT=15451,
        )

        self.assertEqual(
            self.published_ports(stack_a, "db"),
            [
                {
                    "host_ip": "127.0.0.1",
                    "mode": "ingress",
                    "target": 5432,
                    "published": "15450",
                    "protocol": "tcp",
                }
            ],
        )
        self.assertEqual(
            self.published_ports(stack_b, "db"),
            [
                {
                    "host_ip": "127.0.0.1",
                    "mode": "ingress",
                    "target": 5432,
                    "published": "15451",
                    "protocol": "tcp",
                }
            ],
        )

    def test_named_volumes_are_project_prefixed_and_disjoint(self):
        configs = {
            project: self.compose_config(project, ("docker-compose.demo.yml",))
            for project in ("merlon-p00-a", "merlon-p00-b")
        }

        volume_names = {
            project: {
                volume["name"] for volume in config["volumes"].values()
            }
            for project, config in configs.items()
        }
        for project, names in volume_names.items():
            self.assertTrue(names)
            self.assertTrue(all(name.startswith(f"{project}_") for name in names))
        self.assertTrue(volume_names["merlon-p00-a"].isdisjoint(volume_names["merlon-p00-b"]))

    def test_default_api_port_remains_8080(self):
        config = self.compose_config("merlon-p00-a", ("docker-compose.yml",))

        self.assertEqual(
            self.published_ports(config, "api"),
            [{"mode": "ingress", "target": 8080, "published": "8080", "protocol": "tcp"}],
        )


if __name__ == "__main__":
    unittest.main()
