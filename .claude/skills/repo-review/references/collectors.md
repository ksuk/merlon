# Evidence Collectors

Read-only commands to run in audit step 2. Save raw output under
`.audit/evidence-<YYYYMMDD>-<shortsha>/`.

Which evaluation item each command's result feeds into is documented in the
Standard's Part II, "AI agent judgment criteria" subsection for each domain.

Principles:
- Read-only only. Never mutate repository or environment state.
- Redirect large output to a file; put aggregate counts plus the reference path
  in the report, not the raw dump.
- This repository's (Merlon's) layout: Go = `api/`, Rust = `engine/`,
  Node = `ui/` / `website/`, proto = `proto/`, CI = `.github/workflows/`.
  When reusing this skill in another repository, remap the ecosystem-specific
  commands below.

## Common (scoping / repo-wide)

```bash
git rev-parse HEAD                          # pin target revision
git status --porcelain                      # dirty check
git rev-parse --is-shallow-repository       # shallow => history-based items are NE
git ls-files | wc -l                        # population size (for sampling records)
git shortlog -sn --no-merges --since=24.months   # author distribution (D1/D11 — convert names to roles before reporting)
git log --format=%s -200                    # last 200 subject lines (D2)
git tag --sort=creatordate                  # tag list (D2-5/D10-4)
git log --since=12.months --format= --name-only | sort | uniq -c | sort -rn | head -30   # change-frequency ranking = hotspot candidates (D4/D11, sampling input)
```

## D1 Governance

```bash
ls LICENSE* COPYING* NOTICE* README* CONTRIBUTING* CODE_OF_CONDUCT* SECURITY* 2>/dev/null
ls .github/CODEOWNERS CODEOWNERS docs/CODEOWNERS 2>/dev/null
git log -1 --format=%ci -- CONTRIBUTING.md            # governance doc freshness
find . -path ./node_modules -prune -o -type f -size +500k -print | head -20   # clutter / oversized files (D1-6)
```

## D2 Version Control Practices

```bash
git log --merges --oneline -50 | wc -l                # numerator for merge-commit ratio
git log --format=%s -200 | grep -Ec '^(feat|fix|docs|refactor|test|chore|perf|ci|build|style|revert)(\([^)]+\))?!?: '   # Conventional Commits compliance count (out of 200)
git log --shortstat --no-merges -100 | grep -E 'files? changed'   # commit-size distribution (atomicity)
git log --format=%B -100 | grep -c '^Signed-off-by:'  # DCO sign-off rate
# Full-history secret scanning: treat the CI gitleaks (security.yml) result as E3.
# Only if runnable locally: gitleaks git --no-banner .  (never transcribe detected values)
```

## D3 Architecture and Design

```bash
ls docs/architecture.md docs/decisions/ 2>/dev/null && ls docs/decisions/ | tail -5   # ADR presence and recency
git log -1 --format=%ci -- docs/architecture.md       # structural-doc freshness
git log --since=6.months --oneline -- proto/ | head   # external contract (proto) change history (D3-4)
grep -rn "import" api/internal/ --include="*.go" -l | head   # entry point for checking dependency direction (details need judgment-based review)
```

## D4 Implementation Quality

```bash
ls .golangci.yml api/.golangci.yml engine/rustfmt.toml engine/clippy.toml ui/eslint.config.* 2>/dev/null
grep -rn "nolint" api/ --include="*.go" | wc -l        # Go suppression-comment count
grep -rEn "#\[allow\(" engine/ --include="*.rs" | grep -v target | wc -l   # Rust suppression-attribute count
grep -rEn "eslint-disable|@ts-ignore|@ts-expect-error" ui/src website/src 2>/dev/null | wc -l
grep -rEn "TODO|FIXME|HACK" api/ engine/ ui/src --include="*.go" --include="*.rs" --include="*.ts" --include="*.tsx" 2>/dev/null | wc -l
# Empty catch / swallowed-error candidates (hits need context review before classifying):
grep -rEn "catch \{|catch \(.*\) \{ *\}" ui/src website/src 2>/dev/null | head
grep -rn "_ = " api/internal/ --include="*.go" | head   # Go discarded-return-value candidates
```

## D5 Testing and Quality Assurance

```bash
find api -name "*_test.go" | wc -l && find api -name "*.go" ! -name "*_test.go" | wc -l
grep -rn "#\[test\]" engine/crates --include="*.rs" | wc -l
find ui/src -name "*.test.*" -o -name "*.spec.*" | wc -l
grep -n "test" .github/workflows/ci.yml               # CI gate strength (is it a required step)
git log --no-merges --format=%H:%s -100 | grep -Ei ':fix' | head -20   # fix-type commits -> numerator for test-accompaniment rate
# For each fix commit above: git show --stat <hash> to check whether a test file changed
grep -rEn "\.skip|#\[ignore\]|t\.Skip" api engine/crates ui/src 2>/dev/null | head   # disabled tests
```

## D6 Security

```bash
ls SECURITY.md && head -30 SECURITY.md                # check reporting channel / timeline / scope are present
grep -n "gitleaks\|govulncheck\|audit" .github/workflows/security.yml   # CI security-check integration
# Dangerous patterns (hits are candidates — confirm reachability individually before assigning severity):
grep -rn "InsecureSkipVerify" api/ --include="*.go"
grep -rEn "exec\.Command|os/exec" api/internal --include="*.go" | head
grep -rEn "dangerouslySetInnerHTML" ui/src | head
grep -rEn "md5|sha1" api/internal engine/crates --include="*.go" --include="*.rs" | grep -iv "sha256\|test" | head
```

## D7 Dependencies and Supply Chain

```bash
ls api/go.sum engine/Cargo.lock ui/package-lock.json website/package-lock.json   # lockfile presence
ls .github/dependabot.yml .github/renovate.json 2>/dev/null                      # automated update bot config
git log -5 --format=%ci -- api/go.sum engine/Cargo.lock ui/package-lock.json     # lockfile update frequency
grep -n "go 1\." api/go.mod && grep -n "edition" engine/Cargo.toml && grep -n '"node"' ui/package.json website/package.json 2>/dev/null   # runtime versions (only cross-check EOL if outbound lookup is available)
find . -path ./node_modules -prune -o \( -name "*.jar" -o -name "*.so" -o -name "*.dll" \) -print   # vendored/unvetted binaries
```

## D8 CI/CD and Build

```bash
ls .github/workflows/
grep -n "uses:" .github/workflows/*.yml | grep -v "#"   # classify pinning method per reference (@vN tag vs @<sha> digest)
grep -n "go-version\|node-version\|rust-toolchain" .github/workflows/*.yml   # toolchain pinning
grep -n "^[a-z-]*:" Makefile                          # local single entry point vs. CI correspondence (D8-6)
```

## D9 Operability and Observability

```bash
grep -rn "/health\|/ready\|healthz" api/internal --include="*.go" | head   # health-check implementation
ls docs/operations/ && ls config.example.yaml .env.example 2>/dev/null     # operations docs / config examples
grep -rn "MERLON_" api/internal --include="*.go" -l | head                 # entry point for checking env-injected config
grep -rEn "fmt\.Print|println!|console\.log" api/internal engine/crates ui/src 2>/dev/null | grep -v test | wc -l   # debug-residue density
# Runbook validity: extract commands/paths from docs/operations/*.md and cross-check they still exist (per D9's AI judgment criteria)
```

## D10 Documentation and Knowledge

```bash
head -60 README.md                                     # element completeness (purpose/prerequisites/setup/usage)
ls CHANGELOG.md && head -30 CHANGELOG.md && git tag | tail -5   # CHANGELOG-to-tag correspondence
make docs-check                                        # reuse the existing docs gate result (links, language, i18n)
git log -1 --format=%ci -- docs/getting-started.md docs/architecture.md   # freshness of key docs
```

## D11 Maintainability and Evolvability

```bash
git shortlog -sn --no-merges --since=24.months -- api/ | head -5    # per-area concentration (repeat for engine/ and ui/)
git log --format=%s -300 | grep -c '^refactor'         # refactoring continuity
grep -rn "Deprecated\|deprecated" api/internal engine/crates --include="*.go" --include="*.rs" | head   # deprecation lingering (check age via blame)
```

## D12 Compliance and Audit Readiness (regulated profile only)

```bash
git log --format=%s -100 | grep -Ec '#[0-9]+|[A-Z]+-[0-9]+'   # ticket/issue reference rate
ls docs/compliance/                                    # regulatory mapping docs (e.g. fsa-guideline-mapping)
git log -1 --format=%ci -- docs/compliance/            # mapping freshness
# Reverse traceability: latest tag -> git log <prev>..<tag> --oneline -> confirm the reference chain for each change (per the Standard's D12 verification method)
ls migrations/ && grep -rn "retention\|保持" docs/compliance/ | head   # retention policy vs. implementation trace
```
