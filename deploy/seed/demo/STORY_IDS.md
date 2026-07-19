# Synthetic demo story IDs

Fixed IDs for the PH7 demo tour (T5'/T6'). Every name, company, and
transaction below is synthetic (DD3) — regenerate with `make demogen`
(seed=20260701, anchor=2026-07-01).

Each row's **Label** is this generator's internal, human-readable name for
the entity (used only in this document and in the generator's own source);
the actual primary key the HTTP API expects (e.g.
`GET /api/v1/customers/{id}`) is the UUID column, derived deterministically
as `uuidFor(label)` — an RFC 4122 v5 (SHA-1, fixed namespace + label) UUID.
Regenerating reproduces the same UUID for the same label every time.

## A6 stories

| # | Label | Customer ID | Name | Purpose | Alert ID(s) | Case ID | Transactions |
|---|---|---|---|---|---|---|---|
| 1 | demo-story-01 | `d3378686-64ae-5aaa-8057-ecc839f7b07f` | Lina Santos | ストラクチャリング(送金取りまとめ屋) | `99d2824c-86ec-5632-ab6a-341bc6874b06` | `a6bcdeb0-3ad6-576f-a116-c6ab1d8e7ad3` | 32d97737-5417-5fa5-89e1-c9abb962ce6a..fe0255cb-5bad-5c71-9162-362bac534b17 (30) |
| 2 | demo-story-02 | `dcfd883f-d8b0-5966-a693-137e1457b566` | 佐野 巧真 | 高頻度小口取引(売り口座ミュール) | `419d1314-654e-5375-bfb7-9fcea10fcd53` | `ca17b407-5e8b-5b8a-ac41-8150c2524172` | e5862b7a-ac28-5395-8e4e-712a45a32bfd..2fcbeb29-75cc-5dab-b5d3-80b5b1f8d6c6 (28) |
| 3 | demo-story-03 | `bba91f16-80d4-50de-8790-f8a84d2bb221` | 株式会社アオイ貿易 | ハイリスク国送金(中古車輸出) | `7ae5a872-0432-5523-83bf-7f391995f077` | `5947f58c-2c17-5c04-a8f8-12b4d1cb9179` | 05edcf7c-f41d-59f0-83f1-613d2fda23ee..2a07a228-cb93-5500-868c-5493699d9d26 (4) |
| 4 | demo-story-04 | `61a626c6-ced4-536d-be74-41d6ca874e4d` | Meridian Cross Trading Pte. Ltd. | 急速資金移動(パススルー) | `38d7a6ce-c160-5cf3-b748-ce2650893ff3` | `3a55610e-d00f-5a34-8bfa-cc9753cbfa06` | fade97ec-e26f-5253-9440-155b69fe8a5b..2a6b586a-989c-5fbd-b59d-ce1dc43f432e (6) |
| 5 | demo-story-05 | `31ec7fc8-1267-576d-b5de-ee6993633d72` | 平尾 靖子 | 休眠口座再活性化 | `9ec5715d-36d1-554e-8f55-1b459c23fd60` | `e1e77081-f6fd-5e48-a6fd-909923dcec1c` | 27281738-5855-5a1e-a88c-871512e351aa..1a4eb5f1-66db-59d6-a75b-c2f7b7451966 (4) |
| 6 | demo-story-06 | `6f02d79e-133b-5501-8080-a78709b21fb1` | Nguyen Van Phung | 複合(structuring x2 + rapid_movement x1) | `0e790e68-d1db-5355-a74d-49bf4dc74285`, `657a05a6-62d0-5dfe-8ac0-ccea5c6c9a4b`, `6ffed4f6-73f8-5db9-adda-378ae2a82201` | `1b882b69-18db-5a35-95fe-11621167f68d` | da6648ea-abcd-57be-bdc7-98c7d7a9fd54..6a029294-787a-5842-bafd-067bdf1c09df (27) |

## A8 screening hits

| Label | Customer ID | Name | List entry | Status |
|---|---|---|---|---|
| demo-screening-01 | `3b526a5e-c19f-51bd-874a-d53f5da09834` | Demo Subject Alpha | `DEMO-SANCTIONS-001` | REVIEWING |
| demo-screening-02 | `866a309c-3908-56ec-878a-ca21b58bad8d` | Demo Subject Alpha Jr. | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| demo-screening-03 | `14e2d7cc-5e40-596e-8ef6-09f9489659df` | Demo Mining Development Corp. | `DEMO-SANCTIONS-002` | NEW |
| demo-screening-04 | `270bac06-ede6-55e9-8492-0c564db2cbdb` | Demo Asia Logistics Partners | `DEMO-SANCTIONS-003` | FALSE_POSITIVE |
| demo-screening-05 | `e2653972-28d4-5270-b3dd-b3b20338f2fb` | Demo Person Beta | `DEMO-PEP-001` | FALSE_POSITIVE |
| demo-screening-06 | `bdfa4895-72e2-5171-96df-eea88b780c08` | Demo Person Bravo | `DEMO-PEP-001` | FALSE_POSITIVE |
| demo-screening-07 | `640cced1-ca54-5d6d-b5a1-6cfdba2465f6` | Demo Subject Gamma | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| demo-screening-08 | `237499ce-747b-5397-810a-13fe3a2cc2ba` | Demo Mining Ventures Ltd. | `DEMO-SANCTIONS-002` | FALSE_POSITIVE |
| demo-screening-09 | `e0054c7a-cf78-58ea-b5e4-e65932f446af` | Demo Asia Trading Co. | `DEMO-SANCTIONS-003` | FALSE_POSITIVE |
| demo-screening-10 | `41c053d4-24c0-5325-b81e-68fdfc7b58d1` | Demo Subject Delta | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| demo-screening-11 | `bba18d95-f87d-5cf9-8eb9-5ed31125b069` | Demo Person Charlie | `DEMO-PEP-001` | FALSE_POSITIVE |
| demo-screening-12 | `820ffe28-4178-5c3d-a174-f196e4c5ee14` | Demo Kogyo Kaihatsu Co., Ltd. | `DEMO-SANCTIONS-002` | FALSE_POSITIVE |
| demo-screening-13 | `c3430cda-347a-579d-b73d-bc150b1f8f14` | Demo Subject Epsilon | `DEMO-SANCTIONS-001` | FALSE_POSITIVE |
| demo-screening-14 | `ad364a8a-aa34-5fce-b792-41f222fcb674` | Demo Asia Freight Services | `DEMO-SANCTIONS-003` | FALSE_POSITIVE |
| demo-screening-15 | `28136c42-deba-5aa4-a100-b4089c8c80e9` | Demo Person Delta | `DEMO-PEP-001` | FALSE_POSITIVE |

The generator is deterministic (fixed seed/anchor) and keeps these labels
(and therefore these UUIDs) unchanged across regenerations.
