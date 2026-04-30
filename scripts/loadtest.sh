#!/usr/bin/env bash
# Simulates realistic Postgres load for Tusk development.
# Usage: ./scripts/loadtest.sh [duration_seconds]
#
# Creates diverse states visible in Tusk:
# - Fast OLTP (high TPS)
# - Slow analytics (long-running queries)
# - Idle-in-transaction (the bad pattern — visible in :txn)
# - Multi-statement transactions with different txn/query ages
# - Lock contention (visible in :locks)
# - Deadlock attempts
# - Dead tuple churn (visible in :tables dead%)
# - Multiple users (visible in :roles, :connections)
# - SQLcommentor metadata (visible in query detail)
# - Prepared statements
# - Temp table usage
# - Sequential scans on large tables (visible in :tables seq/idx)

set -euo pipefail

DB="postgres://postgres:postgres@localhost:5432/tuskdev?sslmode=disable"
DURATION=${1:-120}
PIDS=()

cleanup() {
    echo ""
    echo "Stopping load generators..."
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null
    echo "Done."
}
trap cleanup EXIT

run_until() {
    local end=$1; shift
    while [ "$(date +%s)" -lt "$end" ]; do
        "$@" 2>/dev/null || true
    done
}

END=$(($(date +%s) + DURATION))

echo "=== Tusk Load Generator ==="
echo "Duration: ${DURATION}s"
echo "Target:   $DB"
echo ""

# ── 1. Fast OLTP reads — different app names for connection variety ──
echo "[+] OLTP readers (3 workers, different apps)..."
for app in checkout-service user-service product-service; do
    (
        run_until "$END" psql "$DB" -c "
            SET application_name = '$app';
            SELECT * FROM app.users WHERE id = (random()*4+1)::int;
        " > /dev/null
    ) &
    PIDS+=($!)
done

# ── 2. OLTP writes — generates TPS and dead tuples ──────────────────
echo "[+] OLTP writers (2 workers)..."
for app in order-processor event-ingester; do
    (
        run_until "$END" psql "$DB" -c "
            SET application_name = '$app';
            INSERT INTO analytics.events (event_type, user_id, payload)
            VALUES (
                (ARRAY['page_view','click','purchase','signup','logout'])[1+(random()*4)::int],
                (random()*4+1)::int,
                jsonb_build_object('ts', now(), 'src', '$app', 'session', gen_random_uuid())
            );
        " > /dev/null
    ) &
    PIDS+=($!)
done

# ── 3. Slow analytics — visible as yellow/red duration ───────────────
echo "[+] Slow analytical queries..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'analytics-dashboard';
        SELECT event_type, count(*), avg(EXTRACT(EPOCH FROM created_at))
        FROM analytics.events
        GROUP BY event_type;
        SELECT pg_sleep(2);
    " > /dev/null
) &
PIDS+=($!)

(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'report-builder';
        SELECT u.name, count(o.id), sum(o.total_cents)
        FROM app.users u
        JOIN app.orders o ON o.user_id = u.id
        JOIN app.order_items oi ON oi.order_id = o.id
        JOIN app.products p ON p.id = oi.product_id
        GROUP BY u.name;
        SELECT pg_sleep(3);
    " > /dev/null
) &
PIDS+=($!)

# ── 4. Very slow query (always red, >30s) ────────────────────────────
echo "[+] Very slow batch job (>30s, always red)..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'nightly-batch';
        SELECT pg_sleep(45);
    " > /dev/null
) &
PIDS+=($!)

# ── 5. Idle-in-transaction — the classic leak pattern ─────────────────
# To get real "idle in transaction" state, we open a transaction, run a
# query, then keep psql open without sending more commands. The connection
# sits in "idle in transaction" state holding locks.
echo "[+] Idle-in-transaction connections (3 — staggered)..."
for i in 1 2 3; do
    (
        {
            echo "SET application_name = 'leaky-service-$i';"
            echo "BEGIN;"
            echo "SELECT * FROM app.users LIMIT 1;"
            # Now just sleep the shell — psql stays open with txn idle
            sleep "$DURATION"
            echo "ROLLBACK;"
        } | psql "$DB" > /dev/null 2>&1 || true
    ) &
    PIDS+=($!)
    sleep 3  # stagger so they have different ages
done

# ── 6. Multi-statement transactions — each statement sent separately ──
# so pg_stat_activity.query changes between polls and Tusk's query
# history captures each one individually.
echo "[+] Multi-statement transactions (3 workers)..."

# Order workflow: 4 distinct queries in one transaction
(
    run_until "$END" bash -c '{
        echo "SET application_name = '\''order-workflow'\'';"
        echo "BEGIN;"
        sleep 0.5
        echo "SELECT * FROM app.users WHERE id = (random()*4+1)::int;"
        sleep 3
        echo "UPDATE app.orders SET status = '\''processing'\'' WHERE id = (random()*7+1)::int;"
        sleep 3
        echo "INSERT INTO analytics.events (event_type, user_id, payload) VALUES ('\''order_complete'\'', (random()*4+1)::int, '\''{}'\''::jsonb);"
        sleep 3
        echo "UPDATE app.orders SET status = '\''completed'\'' WHERE id = (random()*7+1)::int;"
        sleep 2
        echo "COMMIT;"
    } | psql "$0" > /dev/null 2>&1' "$DB"
) &
PIDS+=($!)

# Payment processing: 3 queries with longer sleeps
(
    run_until "$END" bash -c '{
        echo "SET application_name = '\''payment-processor'\'';"
        echo "BEGIN;"
        sleep 1
        echo "SELECT * FROM app.orders WHERE status = '\''processing'\'' LIMIT 1;"
        sleep 4
        echo "UPDATE app.products SET inventory = inventory - 1 WHERE id = (random()*4+1)::int;"
        sleep 4
        echo "INSERT INTO analytics.events (event_type, user_id) VALUES ('\''payment_processed'\'', (random()*4+1)::int);"
        sleep 3
        echo "COMMIT;"
    } | psql "$0" > /dev/null 2>&1' "$DB"
) &
PIDS+=($!)

# Inventory sync: 5 queries stepping through tables
(
    run_until "$END" bash -c '{
        echo "SET application_name = '\''inventory-sync'\'';"
        echo "BEGIN;"
        sleep 1
        echo "SELECT count(*) FROM app.products;"
        sleep 2
        echo "SELECT count(*) FROM app.orders WHERE status = '\''pending'\'';"
        sleep 2
        echo "SELECT count(*) FROM app.order_items;"
        sleep 2
        echo "UPDATE app.products SET inventory = (random()*500)::int WHERE id = (random()*4+1)::int;"
        sleep 2
        echo "INSERT INTO analytics.events (event_type, user_id) VALUES ('\''inventory_synced'\'', 1);"
        sleep 2
        echo "COMMIT;"
    } | psql "$0" > /dev/null 2>&1' "$DB"
) &
PIDS+=($!)

# ── 7. Lock contention — visible in :locks ────────────────────────────
echo "[+] Lock contention (holder + 2 waiters, 15s hold)..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'lock-holder';
        BEGIN;
        UPDATE app.products SET inventory = inventory WHERE id = 1;
        SELECT pg_sleep(15);
        COMMIT;
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

sleep 1
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'lock-waiter-1';
        UPDATE app.products SET inventory = inventory WHERE id = 1;
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

sleep 1
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'lock-waiter-2';
        UPDATE app.products SET inventory = inventory WHERE id = 1;
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

# ── 8. Dead tuple churn — visible in :tables dead% column ─────────────
echo "[+] Dead tuple generator..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'data-churn';
        UPDATE analytics.page_views
        SET duration_ms = duration_ms + 1
        WHERE id IN (
            SELECT id FROM analytics.page_views
            ORDER BY random() LIMIT 2000
        );
    " > /dev/null
    sleep 3
) &
PIDS+=($!)

# ── 9. Sequential scan torture — visible in :tables seq/idx ratio ─────
echo "[+] Sequential scan queries (no index usage)..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'bad-query-pattern';
        SELECT * FROM analytics.page_views WHERE duration_ms > (random()*30000)::int;
        SELECT * FROM analytics.events WHERE payload->>'src' = 'loadtest';
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

# ── 10. Different users — visible in :roles, :connections ─────────────
echo "[+] Queries from different roles..."
(
    run_until "$END" psql "postgres://app_user:apppass@localhost:5432/tuskdev?sslmode=disable" -c "
        SET application_name = 'app-backend';
        SELECT * FROM app.users;
        SELECT * FROM app.orders WHERE status = 'pending';
    " > /dev/null 2>&1
    sleep 1
) &
PIDS+=($!)

(
    run_until "$END" psql "postgres://readonly_user:readonly@localhost:5432/tuskdev?sslmode=disable" -c "
        SET application_name = 'readonly-dashboard';
        SELECT count(*) FROM app.orders;
        SELECT count(*) FROM analytics.events;
    " > /dev/null 2>&1
    sleep 1
) &
PIDS+=($!)

# ── 11. SQLcommentor queries — rich tags visible in query detail ───────
echo "[+] SQLcommentor-annotated queries (6 services)..."

# User service — CRUD operations
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'user-service';
        SELECT * FROM app.users WHERE id = (random()*4+1)::int
        /*app='user-service',route='/api/v1/users/:id',controller='UserController',action='show',framework='gin'*/;
        SELECT pg_sleep(0.3);
    " > /dev/null
) &
PIDS+=($!)

(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'user-service';
        SELECT * FROM app.users ORDER BY created_at DESC LIMIT 20
        /*app='user-service',route='/api/v1/users',controller='UserController',action='index',framework='gin'*/;
        SELECT pg_sleep(0.2);
    " > /dev/null
) &
PIDS+=($!)

# Order service — multi-table joins
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'order-service';
        SELECT o.*, u.name, u.email FROM app.orders o
        JOIN app.users u ON u.id = o.user_id
        WHERE o.id = (random()*7+1)::int
        /*app='order-service',route='/api/v1/orders/:id',controller='OrderController',action='show',framework='gin'*/;
        SELECT pg_sleep(0.3);
    " > /dev/null
) &
PIDS+=($!)

(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'order-service';
        SELECT o.id, o.status, o.total_cents, count(oi.id) as items
        FROM app.orders o
        JOIN app.order_items oi ON oi.order_id = o.id
        WHERE o.user_id = (random()*4+1)::int
        GROUP BY o.id, o.status, o.total_cents
        /*app='order-service',route='/api/v1/users/:id/orders',controller='OrderController',action='by_user',framework='gin'*/;
        SELECT pg_sleep(0.4);
    " > /dev/null
) &
PIDS+=($!)

# Analytics service — heavy aggregations
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'analytics-api';
        SELECT date_trunc('hour', created_at) AS hour, event_type, count(*)
        FROM analytics.events
        WHERE created_at > now() - interval '24 hours'
        GROUP BY hour, event_type
        ORDER BY hour DESC
        /*app='analytics-service',route='/api/v1/analytics/events',controller='AnalyticsController',action='hourly_breakdown',framework='fastapi'*/;
        SELECT pg_sleep(1);
    " > /dev/null
) &
PIDS+=($!)

# Search service — text matching
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'search-service';
        SELECT p.id, p.name, p.price_cents, p.category
        FROM app.products p
        WHERE p.name ILIKE '%widget%' OR p.category = 'gadgets'
        /*app='search-service',route='/api/v1/search',controller='SearchController',action='query',framework='express'*/;
        SELECT pg_sleep(0.2);
    " > /dev/null
) &
PIDS+=($!)

# Notification service — multi-step transaction with tags
(
    run_until "$END" psql "$DB" <<SQL > /dev/null 2>&1
SET application_name = 'notification-service';
BEGIN;
SELECT * FROM app.users WHERE id = (random()*4+1)::int
/*app='notification-service',route='/internal/notify',controller='NotificationWorker',action='send_email'*/;
INSERT INTO analytics.events (event_type, user_id, payload)
VALUES ('email_sent', (random()*4+1)::int, '{"template": "welcome"}')
/*app='notification-service',route='/internal/notify',controller='NotificationWorker',action='log_event'*/;
SELECT pg_sleep(1);
COMMIT;
SQL
    sleep 1
) &
PIDS+=($!)

# ── 12. More multi-query transactions (sent one statement at a time) ──
echo "[+] Multi-query transactions (slow, observable)..."

# Checkout flow — 4 distinct queries, each visible to Tusk for 3s
(
    run_until "$END" bash -c '{
        echo "SET application_name = '\''checkout-flow'\'';"
        echo "BEGIN;"
        sleep 1
        echo "SELECT * FROM app.users WHERE id = (random()*4+1)::int;"
        sleep 3
        echo "SELECT * FROM app.products WHERE id = (random()*4+1)::int;"
        sleep 3
        echo "UPDATE app.orders SET status = '\''processing'\'' WHERE id = (random()*7+1)::int;"
        sleep 3
        echo "UPDATE app.orders SET status = '\''completed'\'' WHERE id = (random()*7+1)::int;"
        sleep 2
        echo "COMMIT;"
    } | psql "$0" > /dev/null 2>&1' "$DB"
) &
PIDS+=($!)

# ETL pipeline — 3 distinct queries
(
    run_until "$END" bash -c '{
        echo "SET application_name = '\''etl-pipeline'\'';"
        echo "BEGIN;"
        sleep 1
        echo "CREATE TEMP TABLE IF NOT EXISTS staging (LIKE analytics.events INCLUDING ALL) ON COMMIT DROP;"
        sleep 3
        echo "INSERT INTO staging SELECT * FROM analytics.events ORDER BY random() LIMIT 100;"
        sleep 3
        echo "SELECT count(*) FROM staging;"
        sleep 3
        echo "COMMIT;"
    } | psql "$0" > /dev/null 2>&1' "$DB"
) &
PIDS+=($!)

# Data sync — 4 distinct queries
(
    run_until "$END" bash -c '{
        echo "SET application_name = '\''data-sync'\'';"
        echo "BEGIN;"
        sleep 1
        echo "SELECT count(*) FROM app.users;"
        sleep 3
        echo "SELECT count(*) FROM app.orders;"
        sleep 3
        echo "SELECT count(*) FROM app.products;"
        sleep 3
        echo "INSERT INTO analytics.events (event_type, user_id, payload) VALUES ('\''sync_complete'\'', 1, '\''{}'\''::jsonb);"
        sleep 2
        echo "COMMIT;"
    } | psql "$0" > /dev/null 2>&1' "$DB"
) &
PIDS+=($!)
    sleep 2
) &
PIDS+=($!)

echo ""
echo "All generators running (${#PIDS[@]} workers). Watch with: make run"
echo "Load will stop in ${DURATION}s or press Ctrl+C"
echo ""

wait
