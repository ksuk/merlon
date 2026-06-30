---
paths:
  - "api/internal/adapter/**"
---
All external system integration differences are absorbed in this adapter layer (Adapter Isolation principle).
Code outside adapters (domain, server, engine) must not depend on external-system-specific knowledge.
