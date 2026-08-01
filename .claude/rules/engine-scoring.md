---
paths:
  - "api/internal/engine/native/**"
  - "content/**"
---
CDD score is the central axis of the system (Score-Driven Architecture). Changes to scoring logic cascade to TM thresholds, case priority, and screening frequency.
Pin deterministic output for identical input via tests (Auditability First).
On failure or error, design toward alerting rather than silence (Fail-Alert).
