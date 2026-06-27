# Contributing to Merlon

Thank you for your interest in contributing to Merlon.

## License

Merlon is licensed under the [Business Source License 1.1](LICENSE) (source-available, not OSI-approved open source). By submitting a contribution, you agree that your contribution will be licensed under the same terms. Contributions do not require copyright assignment.

Files under `content/_sample/` are licensed under [Apache-2.0](content/_sample/LICENSE).

## LEGAL_REVIEW_REQUIRED: Contributor Agreement

A Contributor License Agreement (CLA) or Developer Certificate of Origin (DCO) requirement has not yet been finalized. This section will be updated before accepting external contributions.

## How to Contribute

### Reporting Issues

- Use [GitHub Issues](../../issues) to report bugs or request features
- Check existing issues before creating a new one
- Use the provided issue templates

### Submitting Changes

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Run tests: `make test`
5. Run linter: `make lint`
6. Submit a pull request using the PR template

### Development Setup

See [docs/development/setup.md](docs/development/setup.md) for detailed instructions.

### Code Style

- **Go**: Follow standard `gofmt` formatting. Use `go vet` for static analysis.
- **Rust**: Follow standard `rustfmt` formatting. Use `cargo clippy` for linting.
- **TypeScript**: Follow ESLint configuration in `ui/eslint.config.js`.
- **Proto**: Follow `buf lint` rules (STANDARD).

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `ci`

Examples:
- `feat(scoring): add geographic risk factor`
- `fix(api): handle null customer attributes`
- `docs(adr): add ADR-0005 for event bus selection`

### Confidentiality

Do not include any of the following in contributions:

- Business strategy, pricing, or market analysis
- Risk assessments or vulnerability details
- Customer data or personally identifiable information
- API keys, passwords, or other secrets
