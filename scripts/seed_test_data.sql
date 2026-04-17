-- Test seed: creates markets, positions (open + closed), and trades for an existing user.
-- Usage (pipe from project root):
--   docker compose exec -T postgres psql -U poly -d poly < scripts/seed_test_data.sql
--
-- To override the user, edit the \set seed_user line below, or register a new user
-- via POST /auth/register and look up the UUID in the users table.

\set seed_user 'b2280966-6e73-4883-83c6-082dd6247c25'

BEGIN;

-- ── Wallet (top up if needed) ─────────────────────────────────────────────────

INSERT INTO wallets (user_id, asset_type, token_id, balance, allowance)
VALUES (:'seed_user', 'COLLATERAL', '', 5000, 5000)
ON CONFLICT (user_id, asset_type, token_id) DO UPDATE
  SET balance = wallets.balance + 5000, allowance = wallets.allowance + 5000;

-- ── Markets ───────────────────────────────────────────────────────────────────

INSERT INTO markets (id, condition_id, question, slug, active, closed, neg_risk, tick_size, min_order_size)
VALUES
  ('mkt-trump-2024', 'mkt-trump-2024', 'Will Trump win the 2024 US election?',    'trump-2024', false, true,  false, '0.01', '5'),
  ('mkt-btc-100k',   'mkt-btc-100k',   'Will BTC reach $100k by end of 2025?',    'btc-100k',   false, true,  false, '0.01', '5'),
  ('mkt-fed-cuts',   'mkt-fed-cuts',   'Will the Fed cut rates in Q1 2026?',       'fed-cuts',   true,  false, false, '0.01', '5'),
  ('mkt-eth-flip',   'mkt-eth-flip',   'Will ETH flip BTC by market cap in 2026?','eth-flip',   true,  false, false, '0.01', '5')
ON CONFLICT DO NOTHING;

-- ── Outcome tokens ────────────────────────────────────────────────────────────

INSERT INTO outcome_tokens (token_id, market_id, outcome, winner)
VALUES
  ('tok-trump-yes', 'mkt-trump-2024', 'YES', true),
  ('tok-trump-no',  'mkt-trump-2024', 'NO',  false),
  ('tok-btc-yes',   'mkt-btc-100k',   'YES', false),
  ('tok-btc-no',    'mkt-btc-100k',   'NO',  true),
  ('tok-fed-yes',   'mkt-fed-cuts',   'YES', NULL),
  ('tok-fed-no',    'mkt-fed-cuts',   'NO',  NULL),
  ('tok-eth-yes',   'mkt-eth-flip',   'YES', NULL),
  ('tok-eth-no',    'mkt-eth-flip',   'NO',  NULL)
ON CONFLICT DO NOTHING;

-- ── Orders (UUID ids, needed as FK for trades) ────────────────────────────────

INSERT INTO orders (id, user_id, salt, maker, signer, taker, token_id, maker_amount, taker_amount,
                    side, price, original_size, size_matched, status, order_type, post_only,
                    owner, market, asset_id, outcome, signature)
VALUES
  ('11111111-0001-0000-0000-000000000000', :'seed_user', '1001', '0xSeed', '0xSeed', '0x0',
   'tok-trump-yes', '155000000', '250000000', 'BUY', 0.62, 250, 250, 'MATCHED', 'GTC', false,
   '0xSeed', 'mkt-trump-2024', 'tok-trump-yes', 'YES', '0xsig'),

  ('11111111-0002-0000-0000-000000000000', :'seed_user', '1002', '0xSeed', '0xSeed', '0x0',
   'tok-btc-yes', '137500000', '250000000', 'BUY', 0.55, 250, 250, 'MATCHED', 'GTC', false,
   '0xSeed', 'mkt-btc-100k', 'tok-btc-yes', 'YES', '0xsig'),

  ('11111111-0003-0000-0000-000000000000', :'seed_user', '1003', '0xSeed', '0xSeed', '0x0',
   'tok-fed-yes', '90000000', '200000000', 'BUY', 0.45, 200, 200, 'MATCHED', 'GTC', false,
   '0xSeed', 'mkt-fed-cuts', 'tok-fed-yes', 'YES', '0xsig'),

  ('11111111-0004-0000-0000-000000000000', :'seed_user', '1004', '0xSeed', '0xSeed', '0x0',
   'tok-eth-no', '57600000', '80000000', 'BUY', 0.72, 80, 80, 'MATCHED', 'GTC', false,
   '0xSeed', 'mkt-eth-flip', 'tok-eth-no', 'NO', '0xsig')
ON CONFLICT DO NOTHING;

-- ── Positions ─────────────────────────────────────────────────────────────────

INSERT INTO positions (id, user_id, token_id, market_id, outcome, size, avg_price, realized_pnl, created_at, updated_at)
VALUES
  ('22222222-0001-0000-0000-000000000000', :'seed_user', 'tok-trump-yes', 'mkt-trump-2024', 'YES',
   0, 0.62,  142.80, NOW() - INTERVAL '90 days', NOW() - INTERVAL '60 days'),

  ('22222222-0002-0000-0000-000000000000', :'seed_user', 'tok-btc-yes',   'mkt-btc-100k',   'YES',
   0, 0.55, -137.50, NOW() - INTERVAL '60 days', NOW() - INTERVAL '30 days'),

  ('22222222-0003-0000-0000-000000000000', :'seed_user', 'tok-fed-yes',   'mkt-fed-cuts',   'YES',
   200, 0.45, 0, NOW() - INTERVAL '20 days', NOW() - INTERVAL '5 days'),

  ('22222222-0004-0000-0000-000000000000', :'seed_user', 'tok-eth-no',    'mkt-eth-flip',   'NO',
   80, 0.72,  0, NOW() - INTERVAL '10 days', NOW() - INTERVAL '2 days')
ON CONFLICT DO NOTHING;

-- ── Trades ────────────────────────────────────────────────────────────────────

INSERT INTO trades (id, taker_order_id, user_id, market, asset_id, side, size, fee_rate_bps,
                    price, status, outcome, owner, maker_address, fill_key, match_time)
VALUES
  ('33333333-0001-0000-0000-000000000000', '11111111-0001-0000-0000-000000000000', :'seed_user',
   'mkt-trump-2024', 'tok-trump-yes', 'BUY', '150', '0', '0.62', 'CONFIRMED',
   'YES', '0xSeed', '0xMaker', 'fk-trump-1', NOW() - INTERVAL '92 days'),

  ('33333333-0002-0000-0000-000000000000', '11111111-0001-0000-0000-000000000000', :'seed_user',
   'mkt-trump-2024', 'tok-trump-yes', 'BUY', '100', '0', '0.62', 'CONFIRMED',
   'YES', '0xSeed', '0xMaker', 'fk-trump-2', NOW() - INTERVAL '91 days'),

  ('33333333-0003-0000-0000-000000000000', '11111111-0002-0000-0000-000000000000', :'seed_user',
   'mkt-btc-100k', 'tok-btc-yes', 'BUY', '250', '0', '0.55', 'CONFIRMED',
   'YES', '0xSeed', '0xMaker', 'fk-btc-1', NOW() - INTERVAL '62 days'),

  ('33333333-0004-0000-0000-000000000000', '11111111-0003-0000-0000-000000000000', :'seed_user',
   'mkt-fed-cuts', 'tok-fed-yes', 'BUY', '120', '0', '0.42', 'CONFIRMED',
   'YES', '0xSeed', '0xMaker', 'fk-fed-1', NOW() - INTERVAL '22 days'),

  ('33333333-0005-0000-0000-000000000000', '11111111-0003-0000-0000-000000000000', :'seed_user',
   'mkt-fed-cuts', 'tok-fed-yes', 'BUY', '80', '0', '0.49', 'CONFIRMED',
   'YES', '0xSeed', '0xMaker', 'fk-fed-2', NOW() - INTERVAL '21 days'),

  ('33333333-0006-0000-0000-000000000000', '11111111-0004-0000-0000-000000000000', :'seed_user',
   'mkt-eth-flip', 'tok-eth-no', 'BUY', '80', '0', '0.72', 'CONFIRMED',
   'NO', '0xSeed', '0xMaker', 'fk-eth-1', NOW() - INTERVAL '11 days')
ON CONFLICT DO NOTHING;

COMMIT;

-- ── Verify ────────────────────────────────────────────────────────────────────
SELECT tbl, cnt FROM (
  SELECT 'positions'       AS tbl, COUNT(*)::int AS cnt FROM positions WHERE user_id = :'seed_user'
  UNION ALL
  SELECT 'trades',                 COUNT(*)::int         FROM trades    WHERE user_id = :'seed_user'
  UNION ALL
  SELECT 'open positions',         COUNT(*)::int
    FROM positions p LEFT JOIN outcome_tokens ot ON p.token_id = ot.token_id
    WHERE p.user_id = :'seed_user' AND ot.winner IS NULL
  UNION ALL
  SELECT 'closed positions',       COUNT(*)::int
    FROM positions p LEFT JOIN outcome_tokens ot ON p.token_id = ot.token_id
    WHERE p.user_id = :'seed_user' AND ot.winner IS NOT NULL
) t;
