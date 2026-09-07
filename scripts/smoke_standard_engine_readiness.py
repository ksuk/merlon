"""Exercise standard Compose with missing, invalid, and valid engine roots."""

from __future__ import annotations

import json
import os
from http.cookies import SimpleCookie
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ROOT / "docker-compose.yml"


class StandardComposeSmoke:
    def __init__(self, content_root: Path, port: int, build: bool) -> None:
        self.content_root = content_root
        self.port = port
        self.build = build
        self.project = f"merlon-issue151-{os.getpid()}"
        self.env = os.environ.copy()
        self.env.update(
            {
                "MERLON_POSTGRES_PASSWORD": "issue151-postgres-local-only",
                "MERLON_BOOTSTRAP_TOKEN": "issue151-bootstrap-local-only",
                "MERLON_JWT_SECRET": "issue151-jwt-local-only-32-bytes-minimum",
                "MERLON_API_HOST_PORT": str(port),
                "MERLON_OPERATOR_CONTENT_PATH": str(content_root),
            }
        )
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor())
        self.access_token = ""

    def compose(self, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        command = ["docker", "compose", "-p", self.project, "-f", str(COMPOSE), *args]
        result = subprocess.run(
            command,
            cwd=ROOT,
            env=self.env,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
        )
        if check and result.returncode != 0:
            raise RuntimeError(
                f"{' '.join(command)} failed\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}"
            )
        return result

    def url(self, path: str) -> str:
        return f"http://127.0.0.1:{self.port}{path}"

    def request(self, method: str, path: str, payload: dict | None = None) -> tuple[int, dict]:
        body = None
        headers = {}
        if payload is not None:
            body = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        if self.access_token:
            # The application intentionally accepts JWT sessions through the
            # secure access_token cookie, not as an API-key Bearer header.
            headers["Cookie"] = f"access_token={self.access_token}"
        request = urllib.request.Request(self.url(path), data=body, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=5) as response:
                if path == "/api/v1/auth/login":
                    self.capture_access_token(response.headers.get_all("Set-Cookie", []))
                return response.status, json.loads(response.read())
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read())

    def capture_access_token(self, set_cookies: list[str]) -> None:
        for header in set_cookies:
            cookies = SimpleCookie()
            cookies.load(header)
            if "access_token" in cookies:
                self.access_token = cookies["access_token"].value
                return

    def wait_live(self, timeout: float = 120) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                status, _ = self.request("GET", "/healthz/live")
                if status == 200:
                    return
            except (OSError, ValueError):
                pass
            time.sleep(2)
        raise TimeoutError("standard API did not become live")

    def wait_health(self, expected: str, timeout: float = 90) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            container = self.compose("ps", "-q", "api", check=False).stdout.strip()
            if container:
                result = subprocess.run(
                    ["docker", "inspect", "--format", "{{.State.Health.Status}}", container],
                    capture_output=True,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    check=False,
                )
                if result.stdout.strip() == expected:
                    return
            time.sleep(2)
        raise TimeoutError(f"api container did not become {expected}")

    def run(self) -> None:
        self.compose("up", "--build" if self.build else "--no-build", "--detach")
        try:
            self.wait_live()
        except Exception:
            self.compose("logs", "api", check=False)
            raise

    def stop(self) -> None:
        self.compose("down", "--volumes", "--remove-orphans", check=False)


def make_valid_content(root: Path) -> None:
    (root / "tm_scenarios").mkdir(parents=True)
    (root / "screening_lists").mkdir(parents=True)
    shutil.copy2(ROOT / "content" / "_sample" / "cdd_weights" / "funds_transfer.yaml", root / "cdd_weights.yaml")
    for source in (ROOT / "content" / "_sample" / "tm_scenarios").glob("*.yaml"):
        shutil.copy2(source, root / "tm_scenarios" / source.name)
    for source in (ROOT / "deploy" / "seed" / "demo" / "screening_lists").glob("*.yaml"):
        shutil.copy2(source, root / "screening_lists" / source.name)


def assert_unready_engine(smoke: StandardComposeSmoke) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        status, body = smoke.request("GET", "/healthz/ready")
        if status == 503 and body.get("checks", {}).get("engine") == "error":
            smoke.wait_health("unhealthy")
            return
        time.sleep(2)
    raise AssertionError("readiness did not expose engine=error")


def assert_ready_engine(smoke: StandardComposeSmoke) -> None:
    status, body = smoke.request(
        "POST",
        "/api/v1/setup",
        {"email": "issue151@example.com", "password": "correct-horse-battery-staple"},
    )
    if status != 201:
        raise AssertionError(f"setup status={status} body={body}")
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        status, body = smoke.request("GET", "/healthz/ready")
        if status == 200 and body.get("checks", {}).get("engine") == "ok":
            break
        time.sleep(2)
    else:
        raise AssertionError(f"valid roots did not become ready: status={status} body={body}")

    status, _ = smoke.request(
        "POST",
        "/api/v1/auth/login",
        {"email": "issue151@example.com", "password": "correct-horse-battery-staple"},
    )
    if status != 200:
        raise AssertionError(f"login status={status}")
    status, body = smoke.request("GET", "/api/v1/system/status")
    if status != 200:
        raise AssertionError(f"system status={status} body={body}")
    engine = next(component for component in body["components"] if component["name"] == "engine")
    if engine["operational_state"] != "ready":
        raise AssertionError(f"system status engine={engine}")
    smoke.wait_health("healthy")


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="merlon-issue151-") as temporary:
        base = Path(temporary)
        invalid_cdd = base / "invalid-cdd"
        invalid_tm = base / "invalid-tm"
        invalid_screening = base / "invalid-screening"
        cases = [
            ("missing", base / "missing", 18091, True, assert_unready_engine),
            ("invalid-cdd", invalid_cdd, 18092, False, assert_unready_engine),
            ("invalid-tm", invalid_tm, 18093, False, assert_unready_engine),
            ("invalid-screening", invalid_screening, 18094, False, assert_unready_engine),
            ("valid", base / "valid", 18095, False, assert_ready_engine),
        ]
        (base / "missing").mkdir()
        for invalid_root in (invalid_cdd, invalid_tm, invalid_screening):
            make_valid_content(invalid_root)
        (invalid_cdd / "cdd_weights.yaml").write_text("not: a valid cdd root\n", encoding="utf-8")
        (invalid_tm / "tm_scenarios" / "structuring_basic.yaml").write_text(
            "not: a valid tm scenario\n", encoding="utf-8"
        )
        (invalid_screening / "screening_lists" / "demo_sanctions.yaml").write_text(
            "not: a valid screening list\n", encoding="utf-8"
        )
        make_valid_content(base / "valid")

        for name, content, port, build, assertion in cases:
            smoke = StandardComposeSmoke(content, port, build)
            print(f"[{name}] starting standard Compose")
            try:
                smoke.run()
                assertion(smoke)
                print(f"[{name}] PASS")
            finally:
                smoke.stop()


if __name__ == "__main__":
    main()
