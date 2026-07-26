---
title: Dependency Lifecycle
---

# Dependency Lifecycle

Merlon pins toolchains, base images, GitHub Actions, and security scanners to
reviewed versions or immutable digests. A pin is reproducible evidence, not a
reason to stop updating. This register was last reviewed on **July 16, 2026**.

## Runtime Register

| Component | Repository pin | Upstream support boundary | Required action |
|---|---|---|---|
| Go | 1.25.12 | [Go supports a major release until two newer major releases exist](https://go.dev/doc/devel/release); 1.25 therefore ends support when 1.27 is released | Move to a supported minor immediately for security releases and complete the 1.26 migration before 1.27 is released. |
| Node.js | 22.23.1 | [Node.js 22 Maintenance LTS ends April 30, 2027](https://github.com/nodejs/Release#release-schedule) | Move to the current supported patch promptly and complete migration to a newer LTS at least 90 days before EOL. |
| PostgreSQL | 18.4 | [PostgreSQL 18 support ends November 14, 2030](https://www.postgresql.org/support/versioning/) | Apply supported minor releases after migration and restore testing; complete the next-major upgrade at least 180 days before EOL. |
| Alpine Linux | 3.24.1 | [Alpine 3.24 support ends June 1, 2028](https://www.alpinelinux.org/releases/) | Rebuild on security and maintenance releases and move branches at least 90 days before EOL. |
| nginx | 1.30.4 stable | [nginx publishes current stable and security releases](https://nginx.org/en/download.html) without a fixed EOL date | Track the current stable patch and security advisories; review any stable-series replacement within 30 days. |

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
