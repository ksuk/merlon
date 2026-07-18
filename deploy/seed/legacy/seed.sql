-- Merlon demo seed data.
-- For local development and demos only. Do not load into production.
-- Customers, rule definitions, and score history matching deploy/seed/demo_customers.json.

BEGIN;

-- Customers: 2 individuals, 2 domestic corporations, 1 foreign corporation.
INSERT INTO customers (id, customer_type, name, name_kana, country, risk_tier, occupation_risk, account_opened_at, created_at)
VALUES
  ('11111111-1111-1111-1111-111111111111', 'individual',         '田中 太郎', 'タナカ タロウ',   'JP', 'low',    'low',    '2019-04-01', NOW()),
  ('22222222-2222-2222-2222-222222222222', 'individual',         '鈴木 花子', 'スズキ ハナコ',   'JP', 'medium', 'medium', '2024-09-15', NOW()),
  ('33333333-3333-3333-3333-333333333333', 'corporate_domestic', '株式会社山田商事',  'ヤマダショウジ',   'JP', 'low',    'low',    '2015-06-01', NOW()),
  ('44444444-4444-4444-4444-444444444444', 'corporate_domestic', '佐藤エンタープライズ株式会社', 'サトウエンタープライズ', 'JP', 'medium', 'medium', '2022-01-20', NOW()),
  ('55555555-5555-5555-5555-555555555555', 'corporate_foreign',  'Global Trade Partners Ltd.', NULL,        'SG', 'high',   'high',   '2025-03-10', NOW());

-- Rule definitions referencing the sample content packs.
INSERT INTO rule_definitions (id, rule_type, content_id, schema_version, version, source_path, is_sample, created_at)
VALUES
  ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'tm_scenario', 'tm_structuring_basic', 'tm_scenario_v1', 1, 'content/_sample/tm_structuring.json',    TRUE, NOW()),
  ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 'cdd_weight',  'cdd_basic_weights',    'cdd_weight_v1',  1, 'content/_sample/cdd_basic_weights.yaml', TRUE, NOW());

-- Score history: one record per customer, scored with cdd_basic_weights.
INSERT INTO customer_score_history (id, customer_id, weight_rule_id, score, risk_tier, scored_at)
VALUES
  ('cccccccc-0001-0000-0000-000000000001', '11111111-1111-1111-1111-111111111111', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 1.4, 'low',    NOW()),
  ('cccccccc-0002-0000-0000-000000000002', '22222222-2222-2222-2222-222222222222', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 2.8, 'medium', NOW()),
  ('cccccccc-0003-0000-0000-000000000003', '33333333-3333-3333-3333-333333333333', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 1.9, 'low',    NOW()),
  ('cccccccc-0004-0000-0000-000000000004', '44444444-4444-4444-4444-444444444444', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 3.1, 'medium', NOW()),
  ('cccccccc-0005-0000-0000-000000000005', '55555555-5555-5555-5555-555555555555', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 4.2, 'high',   NOW());

COMMIT;
