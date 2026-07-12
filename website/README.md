# Website

This website is built using [Docusaurus](https://docusaurus.io/), a modern static website generator.

## Installation

```bash
npm install
```

**Note**: feel free to use the package manager of your choice.

## Local Development

```bash
npm run start
```

This command starts a local development server and opens up a browser window. Most changes are reflected live without having to restart the server.

## Build

```bash
npm run build
```

This command generates static content into the `build` directory and can be served using any static contents hosting service.

## i18n

- `../docs/` is the **English canonical source** for all documentation.
- Real Japanese translations live under
  `i18n/ja/docusaurus-plugin-content-docs/current/`, mirroring the `docs/`
  directory structure.
- Untranslated pages are filled in at build time: `npm run sync:i18n` copies
  every English doc that has no Japanese counterpart into the `ja` tree so
  that relative links resolve in both locales. These fallback copies are
  tracked in `.i18n-fallback-manifest.json` (gitignored) and **must not be
  committed** — `npm run build` runs the sync automatically via `prebuild`.
  The sync also regenerates `i18n/.gitignore` (committable, do not edit) from
  that manifest, so the copies are invisible to `git add` while real
  translations stay visible.
- To add a new translation, just add the translated file at the matching
  path under `i18n/ja/docusaurus-plugin-content-docs/current/`; the next
  sync will stop shadowing it with the English copy.

## Deployment

Using SSH:

```bash
USE_SSH=true npm run deploy
```

Not using SSH:

```bash
GIT_USER=<Your GitHub username> npm run deploy
```

If you are using GitHub Pages for hosting, this command is a convenient way to build the website and push to the `gh-pages` branch.
