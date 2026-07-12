# Contributing to Merlon

Thank you for your interest in contributing to Merlon.

## License

Merlon is licensed under the [Business Source License 1.1](LICENSE) (source-available, not OSI-approved open source). By submitting a contribution, you agree that your contribution will be licensed under the same terms. Contributions do not require copyright assignment.

Files under `content/_sample/` are licensed under [Apache-2.0](content/_sample/LICENSE).

## Developer Certificate of Origin

All commits submitted to Merlon must include a Developer Certificate of Origin (DCO) 1.1 sign-off. By signing off, you certify that you have the right to submit the contribution under this repository's license. Use `git commit -s` to add a `Signed-off-by:` trailer to each commit. See the [Developer Certificate of Origin 1.1](https://developercertificate.org/) for the full text.

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

Use a descriptive branch prefix: `feat/`, `fix/`, `docs/`, `refactor/`,
`test/`, `chore/`, `ci/`, or `perf/`.

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
