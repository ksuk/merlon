---
paths:
  - "api/internal/engine/**"
  - "content/schema/**"
---
When changing the native engine or content schemas, run the focused Go tests
(`cd api && go test ./internal/engine/...`) and the parity corpus replay.
Changes to the public REST contract must update the OpenAPI export and its
documentation in the same change.
