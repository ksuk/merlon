---
title: Dependency Lifecycle
---

# Dependency Lifecycle

Merlon pins toolchains, base images, GitHub Actions, and security scanners to
reviewed versions or immutable digests. A pin is reproducible evidence, not a
reason to stop updating. This register was last reviewed on **August 15, 2026**.

## Runtime Register

| Component | Repository pin | Upstream support boundary | Required action |
|---|---|---|---|
| Go | 1.26.6 | [Go supports a major release until two newer major releases exist](https://go.dev/doc/devel/release); 1.26 therefore ends support when 1.28 is released | Move to a supported minor immediately for security releases and complete the 1.27 migration before 1.28 is released. |
| Node.js | 26.5.0 | [Node.js 26 is Current until it enters Active LTS on October 28, 2026, and ends April 30, 2029](https://github.com/nodejs/Release#release-schedule) | Move to the current supported patch promptly. Until the Active LTS date, treat Node.js as a fast-moving line and re-check upstream on every release; complete migration to a newer LTS at least 90 days before EOL. |
| PostgreSQL | 18.4 | [PostgreSQL 18 support ends November 14, 2030](https://www.postgresql.org/support/versioning/) | Apply supported minor releases after migration and restore testing; complete the next-major upgrade at least 180 days before EOL. |
| Alpine Linux | 3.24.1 | [Alpine 3.24 support ends June 1, 2028](https://www.alpinelinux.org/releases/) | Rebuild on security and maintenance releases and move branches at least 90 days before EOL. |

The operator UI is served by the Go binary from `MERLON_UI_DIR`, so the runtime
image carries no separate web server and this register tracks no web-server
component.

## Review Process

- Dependabot checks Go modules, npm packages, GitHub Actions, and Docker files
  weekly. Action references remain full commit SHAs after review.
- The security owner reviews upstream security notices and patch availability
  monthly, and reviews this EOL register quarterly. A published vulnerability,
  upstream EOL change, or unsupported CI runner triggers an out-of-cycle review.
- Runtime updates use a public issue and PR, record upstream release notes,
  preserve image digests, run all required workflows, and include migration and
  rollback impact. Major PostgreSQL updates additionally require backup and
  restore evidence.
- A release cannot proceed with an EOL runtime or an unresolved critical or
  high vulnerability unless the security policy permits a time-bounded,
  independently approved exception with compensating controls.

Dates above are upstream schedules and can change. The release checklist must
record a fresh official-source review rather than relying only on this page.
