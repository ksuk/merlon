@AGENTS.md

## Claude Code specifics

- Path-scoped rules live in `.claude/rules/`. They load when Claude reads a file matching their `paths` frontmatter.
- Repository quality audits run via the `/repo-review` skill (`.claude/skills/repo-review/`), which evaluates against `docs/standards/repository-quality-review-standard.md`.
- Shared plugin enablement is committed in `.claude/settings.json`. Keep personal permissions and overrides in `.claude/settings.local.json`, which is gitignored.
