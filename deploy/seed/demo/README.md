# Merlon demo seed

The deterministic SQL is generated during the demo image build with:

```sh
go run ./api/cmd/merlon-demogen -output deploy/seed/demo/demo_seed.sql
```

The fixed seed is `20260701` and the anchor timestamp is `2026-07-01T00:00:00Z`.
The generated file is consumed only by `merlon-demo-reset`; it is not loaded by
the API startup path.

