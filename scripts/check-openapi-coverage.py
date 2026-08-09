#!/usr/bin/env python3
"""Report and ratchet OpenAPI coverage of the registered API surface.

The OpenAPI document is the contract Merlon publishes, and ADR-driven policy
requires that contract to stay backward compatible for 12+ months. A route that
is registered but absent from the document is outside that promise while still
being callable: integrators find it, depend on it, and discover only later that
nothing committed to keeping it.

Roughly a third of the surface is currently undocumented. Specifying all of it
in one change would be a large, unreviewable diff, so this guard does the two
things that are useful immediately:

  * It names the undocumented routes, so the gap is a list rather than a
    rumour.
  * It ratchets. Coverage may rise, and may not fall below the baseline
    recorded in this file. Adding a route without documenting it fails.

Lower the baseline only when a route is deliberately removed, and say so in the
commit. Raising it is the point.

Uses only the standard library so it runs in CI without an install step.
"""

from __future__ import annotations

import hashlib
import json
import pathlib
import re
import sys

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
ROUTES_FILE = REPO_ROOT / "api" / "internal" / "server" / "server.go"
SPEC_FILE = REPO_ROOT / "docs" / "api" / "openapi.json"

# The number of registered API operations currently present in the OpenAPI
# document. This may only be raised. See the module docstring.
BASELINE_DOCUMENTED = 130

# The total number of registered API operations. Pinning this separately from
# documented coverage makes an undocumented addition visible: without it,
# covered would remain at BASELINE_DOCUMENTED while the missing list silently
# grew. Any intentional surface change must update this value in review.
BASELINE_REGISTERED = 130

# SHA-256 of the normalized, sorted route sets at the baseline above. Counts
# catch simple additions/removals; these digests also catch a same-count
# replacement, which would otherwise let a new undocumented route trade places
# with an old one without failing CI.
BASELINE_REGISTERED_SHA256 = "af38b65e6d493b38c9044bf0f78f54cc9ffe7b0e49059c31faa0f9fe9d6e9a68"
BASELINE_UNDOCUMENTED = 0
BASELINE_UNDOCUMENTED_SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

# Only /api/ paths form the published contract. Operational probes (/healthz,
# /metrics) and the SPA asset routes are deliberately outside it, and are
# filtered from both sides so the comparison comes out even.
CONTRACT_PREFIX = "/api/"

HTTP_METHODS = frozenset({"get", "put", "post", "delete", "patch", "head", "options", "trace"})

# s.route("GET /api/v1/customers", ...) / s.routeHandler("POST /api/v1/rules", ...)
ROUTE_RE = re.compile(r's\.(?:route|routeHandler)\(\s*"([A-Z]+)\s+([^"]+)"')


def registered_routes(source: str) -> set[tuple[str, str]]:
    """Every contract (method, path) the server registers."""
    return {
        (method.upper(), normalize(path))
        for method, path in ROUTE_RE.findall(source)
        if path.startswith(CONTRACT_PREFIX)
    }


def documented_routes(spec: dict) -> set[tuple[str, str]]:
    """Every contract (method, path) the OpenAPI document describes."""
    servers = spec.get("servers") or [{}]
    base = (servers[0].get("url") or "").rstrip("/")

    routes = set()
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        full = f"{base}{path}"
        if not full.startswith(CONTRACT_PREFIX):
            continue
        for method in item:
            if method.lower() in HTTP_METHODS:
                routes.add((method.upper(), normalize(full)))
    return routes


def normalize(path: str) -> str:
    """Reduce a path to its shape, so {id} and {customerId} compare equal.

    Go's ServeMux and OpenAPI both use brace placeholders but do not agree on
    what to call them, and a name-sensitive comparison would report a
    documented route as missing purely because the spec calls the parameter
    something else.
    """
    path = re.sub(r"\{[^}]*\}", "{}", path)
    return path.rstrip("/") or "/"


def route_set_digest(routes: set[tuple[str, str]]) -> str:
    """Return a stable digest of a normalized route set."""
    payload = "".join(f"{method} {path}\n" for method, path in sorted(routes))
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def main() -> int:
    try:
        source = ROUTES_FILE.read_text(encoding="utf-8")
        spec = json.loads(SPEC_FILE.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        print(f"cannot read {exc.filename}", file=sys.stderr)
        return 1

    registered = registered_routes(source)
    documented = documented_routes(spec)

    # Anti-vacuous: an extraction that silently stops matching would make every
    # comparison below trivially pass.
    if not registered:
        print("extracted no registered routes; this guard cannot compare anything", file=sys.stderr)
        return 1
    if not documented:
        print("extracted no documented routes; this guard cannot compare anything", file=sys.stderr)
        return 1

    covered = registered & documented
    missing = sorted(registered - documented)
    stale = sorted(documented - registered)
    registered_digest = route_set_digest(registered)
    undocumented_digest = route_set_digest(set(missing))

    print(f"OpenAPI coverage: {len(covered)}/{len(registered)} registered API operations documented")

    status = 0

    if registered_digest != BASELINE_REGISTERED_SHA256:
        if len(registered) != BASELINE_REGISTERED:
            direction = "rose" if len(registered) > BASELINE_REGISTERED else "fell"
            print(
                f"\nregistered route count {direction} to {len(registered)}, "
                f"from the baseline of {BASELINE_REGISTERED}.",
                file=sys.stderr,
            )
        else:
            print(
                f"\nregistered route set changed while the count remained "
                f"{BASELINE_REGISTERED}.",
                file=sys.stderr,
            )
        print(
            "Document every added route, then update BASELINE_REGISTERED and "
            "BASELINE_REGISTERED_SHA256 deliberately for the reviewed API "
            f"surface change (current digest: {registered_digest}).",
            file=sys.stderr,
        )
        status = 1

    if undocumented_digest != BASELINE_UNDOCUMENTED_SHA256:
        if len(missing) != BASELINE_UNDOCUMENTED:
            print(
                f"\nundocumented route count changed to {len(missing)}, from "
                f"the baseline of {BASELINE_UNDOCUMENTED}.",
                file=sys.stderr,
            )
        else:
            print(
                f"\nundocumented route set changed while the count remained "
                f"{BASELINE_UNDOCUMENTED}.",
                file=sys.stderr,
            )
        print(
            "Review the missing-route list, then update "
            "BASELINE_UNDOCUMENTED and BASELINE_UNDOCUMENTED_SHA256 "
            f"deliberately (current digest: {undocumented_digest}).",
            file=sys.stderr,
        )
        if missing:
            print("Current undocumented routes:", file=sys.stderr)
            for method, path in missing:
                print(f"  {method} {path}", file=sys.stderr)
        status = 1

    if stale:
        print("\ndocumented in openapi.json but not registered by the server:", file=sys.stderr)
        for method, path in stale:
            print(f"  {method} {path}", file=sys.stderr)
        print("A documented route that does not exist is worse than an undocumented "
              "one: integrators build against it.", file=sys.stderr)
        status = 1

    if len(covered) < BASELINE_DOCUMENTED:
        print(
            f"\ncoverage fell to {len(covered)}, below the baseline of {BASELINE_DOCUMENTED}.",
            file=sys.stderr,
        )
        print(
            "Document the route in api/internal/server/openapi.go "
            "(BuildOpenAPISpec), or lower the baseline deliberately if a "
            "route was removed.",
            file=sys.stderr,
        )
        for method, path in missing:
            print(f"  undocumented: {method} {path}", file=sys.stderr)
        status = 1
    elif len(covered) > BASELINE_DOCUMENTED:
        print(
            f"\ncoverage rose to {len(covered)}. Raise BASELINE_DOCUMENTED in "
            f"{pathlib.Path(__file__).name} to {len(covered)} to lock it in.",
            file=sys.stderr,
        )
        status = 1

    if missing and status == 0:
        print(f"\n{len(missing)} registered operations are not in the published contract:")
        for method, path in missing:
            print(f"  {method} {path}")
        print("\nThese are callable but carry no compatibility promise. Reducing "
              "this list is the goal; the baseline stops it from growing.")

    return status


if __name__ == "__main__":
    sys.exit(main())
