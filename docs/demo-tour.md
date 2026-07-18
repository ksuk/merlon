# Merlon demo tour

The tour is deliberately short and uses only synthetic records. Run it
locally against `docker compose -f docker-compose.demo.yml up` at
`http://localhost:8080`; any changes made while following it persist until you
tear the stack down.

## Path A — compliance reviewer (7–10 minutes)

1. Open the dashboard and select the highest-severity alert.
2. In the alert detail, open the linked customer and transaction. The
   scenario ID is synthetic and can be followed to the rules view.
3. On the customer page, review the CDD score factors and run **Score again**.
4. Create a case from the alert, add a short synthetic note, and change its
   status. Story record IDs are fixed at generation time; see
   [`STORY_IDS.md`](../deploy/seed/demo/STORY_IDS.md).
5. Create an STR draft from the report flow and inspect the audit entry. Do
   not use real identities or transaction descriptions.

## Path B — technical evaluator (5–7 minutes)

1. Open **Rules** and inspect a scenario definition as JSON.
2. Run a customer re-score and confirm the score history records the rule
   set ID and version.
3. Open **Backtest** and run a small synthetic selection.
4. Open **Batch** and run the smallest available score/monitor batch.
5. Finish on **Audit** and **System**. Confirm the displayed version,
   enabled features, and component health.

## Local launch

```bash
docker compose -f docker-compose.demo.yml up --build
```

This starts `db` and `api` with authentication disabled and the demo dataset
seeded, bound to `127.0.0.1:8080`. Tear the stack down (and drop the
synthetic data) with:

```bash
docker compose -f docker-compose.demo.yml down -v
```
