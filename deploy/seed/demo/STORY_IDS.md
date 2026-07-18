# Synthetic demo story IDs

These IDs are stable across resets for deep links and acceptance checks. They
contain no real-person or real-company identifiers.

| Story | Case ID | Customer | Purpose |
|---|---|---|---|
| 1 | `demo-case-001` | `DEMO-CUSTOMER-0001` | Synthetic investigation |
| 2 | `demo-case-002` | `DEMO-CUSTOMER-0041` | Synthetic investigation |
| 3 | `demo-case-003` | `DEMO-CUSTOMER-0081` | Synthetic investigation |
| 4 | `demo-case-004` | `DEMO-CUSTOMER-0121` | Synthetic investigation |
| 5 | `demo-case-005` | `DEMO-CUSTOMER-0161` | Synthetic investigation |
| 6 | `demo-case-006` | `DEMO-CUSTOMER-0201` | Synthetic investigation |

The SQL generator is deterministic (`seed=20260701`) and keeps these IDs
unchanged after every reset.
