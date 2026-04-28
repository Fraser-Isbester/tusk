-- Simulated workload queries for Tusk development.
-- Run with: psql $TUSK_DB -f scripts/workload.sql

-- 1. Slow aggregation — sits for a few seconds
SELECT pg_sleep(0.5), count(*), event_type
FROM analytics.events
GROUP BY event_type;

-- 2. Join-heavy query
SELECT u.name, count(o.id) AS order_count, sum(o.total_cents) AS total_spent
FROM app.users u
JOIN app.orders o ON o.user_id = u.id
JOIN app.order_items oi ON oi.order_id = o.id
GROUP BY u.name
ORDER BY total_spent DESC;

-- 3. Sequential scan on big table
SELECT path, count(*) AS views, avg(duration_ms)::int AS avg_ms
FROM analytics.page_views
WHERE referrer IS NOT NULL
GROUP BY path
ORDER BY views DESC;

-- 4. Simulated long query
SELECT pg_sleep(3), 'long running query' AS label;

-- 5. Subquery with window function
SELECT * FROM (
    SELECT
        event_type,
        user_id,
        created_at,
        row_number() OVER (PARTITION BY user_id ORDER BY created_at DESC) AS rn
    FROM analytics.events
) sub
WHERE rn <= 5;

-- 6. Update that touches many rows (creates dead tuples)
UPDATE analytics.page_views
SET duration_ms = duration_ms + 1
WHERE id IN (SELECT id FROM analytics.page_views ORDER BY random() LIMIT 1000);

-- 7. Another slow one
SELECT pg_sleep(2), avg(price_cents) FROM app.products;
