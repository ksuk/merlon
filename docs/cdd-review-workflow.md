---
title: CDD Periodic Review Workflow
sidebar_position: 9
---

# CDD Periodic Review Workflow

The review queue is a durable operational control. `customer_reviews` keeps
every cycle and its policy version/digest, assignment, scope, rationale,
evidence references, score links, and completion actor. A unique
`(customer_id, cycle)` key makes the daily sweep idempotent. The current next
and last dates are projected onto `customers`; the history row remains the
source of truth.

## Lifecycle

Rows move through `scheduled`, `due`, `overdue`, `in_progress`, `blocked`, and
`completed`. Starting or assigning a row uses its `version` as an optimistic
lock. `unable_to_complete` records the rationale and evidence but leaves the
row `blocked` and does not advance the next review date.

Analysts and Administrators may complete every risk tier. Completion requires
a rationale, a non-empty evidence reference list, and a structured scope. A
rating-changing completion runs the CDD engine and links the resulting score
history record. The review row, customer projection, score history, audit
entry, and `customer.review.completed` outbox event are written through the
same transaction boundary; a score, audit, or outbox failure leaves the review
uncompleted.

## Operator API

* `GET /api/v1/customer-reviews` — filter by status, tier, assignment/team,
  due/overdue, and cursor.
* `GET /api/v1/customer-reviews/{id}` — inspect one cycle and its evidence.
* `PATCH /api/v1/customer-reviews/{id}` — assign, start, or resume with
  `expected_version`.
* `POST /api/v1/customer-reviews/{id}/complete` — append completion evidence
  and optionally produce a new CDD score.

The customer detail view links to review history. The dashboard and review
queue expose due, overdue, and deterministic cold-start backlog counts.
