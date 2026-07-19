# Synthetic demo story IDs

Fixed IDs for the PH7 demo tour (T5'/T6'). Every name, company, and
transaction below is synthetic (DD3) — regenerate with `make demogen`
(seed=20260701, anchor=2026-07-01).

## A6 stories

| # | Customer ID | Name | Purpose | Alert ID(s) | Case ID | Transactions |
|---|---|---|---|---|---|---|
| 1 | `demo-story-01` | Lina Santos | ストラクチャリング(送金取りまとめ屋) | `demo-alert-00005` | `demo-case-01` | demo-txn-0000001..demo-txn-0045749 (30) |
| 2 | `demo-story-02` | 佐野 巧真 | 高頻度小口取引(売り口座ミュール) | `demo-alert-00006` | `demo-case-02` | demo-txn-0000025..demo-txn-0045760 (28) |
| 3 | `demo-story-03` | 株式会社アオイ貿易 | ハイリスク国送金(中古車輸出) | `demo-alert-00007` | `demo-case-03` | demo-txn-0000042..demo-txn-0045763 (4) |
| 4 | `demo-story-04` | Meridian Cross Trading Pte. Ltd. | 急速資金移動(パススルー) | `demo-alert-00008` | `demo-case-04` | demo-txn-0000043..demo-txn-0045766 (6) |
| 5 | `demo-story-05` | 平尾 靖子 | 休眠口座再活性化 | `demo-alert-00009` | `demo-case-05` | demo-txn-0000046..demo-txn-0045769 (4) |
| 6 | `demo-story-06` | Nguyen Van Phung | 複合(structuring x2 + rapid_movement x1) | `demo-alert-00010`, `demo-alert-00011`, `demo-alert-00012` | `demo-case-06` | demo-txn-0000047..demo-txn-0045777 (27) |

## A8 screening hits

| Customer ID | Name | List entry | Status |
|---|---|---|---|
| `demo-screening-01` | Demo Subject Alpha | `DEMO-SANCTIONS-001` | REVIEWING |
| `demo-screening-02` | Demo Subject Alpha Jr. | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| `demo-screening-03` | Demo Mining Development Corp. | `DEMO-SANCTIONS-002` | NEW |
| `demo-screening-04` | Demo Asia Logistics Partners | `DEMO-SANCTIONS-003` | FALSE_POSITIVE |
| `demo-screening-05` | Demo Person Beta | `DEMO-PEP-001` | FALSE_POSITIVE |
| `demo-screening-06` | Demo Person Bravo | `DEMO-PEP-001` | FALSE_POSITIVE |
| `demo-screening-07` | Demo Subject Gamma | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| `demo-screening-08` | Demo Mining Ventures Ltd. | `DEMO-SANCTIONS-002` | FALSE_POSITIVE |
| `demo-screening-09` | Demo Asia Trading Co. | `DEMO-SANCTIONS-003` | FALSE_POSITIVE |
| `demo-screening-10` | Demo Subject Delta | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| `demo-screening-11` | Demo Person Charlie | `DEMO-PEP-001` | FALSE_POSITIVE |
| `demo-screening-12` | Demo Kogyo Kaihatsu Co., Ltd. | `DEMO-SANCTIONS-002` | FALSE_POSITIVE |
| `demo-screening-13` | Demo Subject Epsilon | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| `demo-screening-14` | Demo Asia Freight Services | `DEMO-SANCTIONS-003` | FALSE_POSITIVE |
| `demo-screening-15` | Demo Person Delta | `DEMO-PEP-001` | FALSE_POSITIVE |

The generator is deterministic (fixed seed/anchor) and keeps these IDs
unchanged across regenerations.
