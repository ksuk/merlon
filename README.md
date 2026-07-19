# Merlon

AML/CFT (Anti-Money Laundering / Countering the Financing of Terrorism) compliance software for Japanese non-bank financial institutions.

Merlon provides integrated **Customer Due Diligence (CDD) scoring** and **Transaction Monitoring (TM)** capabilities, designed for self-hosted deployment by crypto-asset exchanges, money transfer operators, and other regulated entities.

It is designed primarily for Japanese non-bank financial institutions, while its configuration-driven model can support banks and organizations in other jurisdictions after their own legal and compliance assessment. Each deployment serves one institution; Merlon is not a multi-tenant service.

## License

Merlon is **source-available** software licensed under the [Business Source License 1.1](LICENSE). It is **not** OSI-approved open source.

- Core software: BSL 1.1
- Sample content (`content/_sample/`): Apache-2.0
- Enterprise features are controlled by license key within the same codebase

Production use is permitted under the Additional Use Grant; offering Merlon to third parties as a hosted, managed, or embedded service is not. See [LICENSE](LICENSE) for the exact terms.

See [docs/decisions/0003-bsl-license-choice.md](docs/decisions/0003-bsl-license-choice.md) for the rationale.

## Architecture

```
┌─────────────────────────────────────────────────┐
│  External Systems (customer's existing infra)   │
└───────────────────────┬─────────────────────────┘
                        │ REST / Webhook
┌───────────────────────▼─────────────────────────┐
│  api/ (Go)                                      │
│  Customer, Transaction, Case, Report Services   │
│  REST API + native scoring/TM/screening engine  │
└───────────────────────┬─────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────┐
│  Data Layer                                     │
│  PostgreSQL (primary) / Redis (cache, optional) │
└─────────────────────────────────────────────────┘
        ▲
┌───────┴─────────────────────────────────────────┐
│  ui/ (TypeScript + React)                       │
│  Operator Dashboard                             │
└─────────────────────────────────────────────────┘
```

See [docs/architecture.md](docs/architecture.md) for details.

## Quick Start

Prerequisites: [Docker](https://docs.docker.com/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/)

```bash
git clone https://github.com/ksuk/merlon.git
cd merlon
cp .env.example .env
docker compose -f docker-compose.minimal.yml up --build
```

Verify:

```bash
curl http://localhost:8080/healthz
# {"status":"ok","version":"dev"}
```

See [docs/getting-started.md](docs/getting-started.md) for the full guide.

## Demo

Want to try Merlon without your own data? A local demo stack ships with a
synthetic dataset (about 1,015 customers, 98 alerts) and authentication
disabled, so you can click through scoring, alerts, cases, and reports
immediately.

```bash
docker compose -f docker-compose.demo.yml up --build
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). Follow
[docs/demo-tour.md](docs/demo-tour.md) for a guided walkthrough of both a
compliance-reviewer path and a technical-evaluator path.

This dataset is entirely synthetic — never load real customer or transaction
data into it, and never run this compose file on a publicly reachable host;
it has no authentication.

Prefer not to use Docker? See "Running without Docker" in
[docs/demo-tour.md](docs/demo-tour.md#running-without-docker) for the
in-memory equivalent.

## Development

Requirements: Go 1.25+, Node.js 20+

```bash
# Run all tests
make test

# Build all components
make build

# Start development environment
make dev-up
```

See [docs/development/setup.md](docs/development/setup.md) for detailed setup instructions. Alternatively, use the [Dev Container](.devcontainer/) for a pre-configured environment.

## Documentation

| Document | Description |
|---|---|
| [Getting Started](docs/getting-started.md) | Installation and first run |
| [Architecture](docs/architecture.md) | System design overview |
| [Configuration](docs/configuration.md) | All configuration options |
| [FSA Guideline Mapping](docs/compliance/fsa-guideline-mapping.md) | Regulatory compliance coverage |
| [Development Setup](docs/development/setup.md) | Local development environment |
| [ADRs](docs/decisions/) | Architecture decision records |

## Status

This project is in active development (M18). Core backend services (Go API with native rule evaluation), operator dashboard UI with full API coverage, code splitting, error boundaries, and development mock data seeder are implemented.

## Production Warning

**This software is intended for AML/CFT compliance operations.** Production use requires:

- Proper rule configuration and validation for your specific regulatory requirements
- Legal and compliance review by qualified professionals
- Thorough testing with your organization's data and workflows
- Ongoing maintenance and regulatory updates

The developers of this software are not responsible for regulatory compliance decisions made using this tool.
