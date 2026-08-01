# React UI

## Commands

```bash
cd ui && npm run dev              # Dev server (Vite, localhost:3000)
cd ui && npm run build            # tsc + vite build
cd ui && npm run test             # Vitest watch mode
cd ui && npm run test -- --run    # Single run
cd ui && npm run lint             # ESLint
```

## Structure

| Directory | Role |
|---|---|
| `src/pages/` | Page components (mapped to React Router routes) |
| `src/components/ui/` | Shared UI components (shadcn/ui pattern) |
| `src/components/layout/` | Layout (sidebar, AppLayout) |
| `src/hooks/` | Custom hooks (`use-api.ts`, etc.) |
| `src/lib/` | Utilities (`api.ts`, `utils.ts`) |

## Patterns

- Tests: Vitest + Testing Library + jsdom. Files named `*.test.tsx`
- Code splitting: all pages lazy-loaded via `React.lazy()` in `App.tsx`
- Styling: Tailwind CSS v4 + `cn()` utility (clsx + tailwind-merge)
- Path alias: `@/` → `src/`
