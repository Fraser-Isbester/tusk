#!/usr/bin/env bash
# Simulates realistic Postgres load for Tusk development.
# Usage: ./scripts/loadtest.sh [duration_seconds]
#
# Spawns concurrent background processes that create:
# - Fast OLTP queries (high QPS)
# - Slow analytical queries
# - Idle-in-transaction connections
# - Lock contention scenarios
# - Dead tuple churn (for vacuum stats)
# - Multiple application_name sources (for connection grouping)

set -euo pipefail

DB="postgres://postgres:postgres@localhost:5432/tuskdev?sslmode=disable"
DURATION=${1:-60}
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

# ── 1. Fast OLTP reads (high QPS) ────────────────────────────
echo "[+] Starting OLTP readers (3 workers)..."
for i in 1 2 3; do
    (
        export PGAPPNAME="oltp-reader-$i"
        run_until "$END" psql "$DB" -c "
            SET application_name = '$PGAPPNAME';
            SELECT * FROM app.users WHERE id = (random()*4+1)::int;
            SELECT * FROM app.products WHERE id = (random()*4+1)::int;
            SELECT o.id, o.total_cents, o.status FROM app.orders o
                JOIN app.users u ON u.id = o.user_id
                WHERE u.id = (random()*4+1)::int;
        " > /dev/null
    ) &
    PIDS+=($!)
done

# ── 2. Fast OLTP writes ──────────────────────────────────────
echo "[+] Starting OLTP writers (2 workers)..."
for i in 1 2; do
    (
        export PGAPPNAME="oltp-writer-$i"
        run_until "$END" psql "$DB" -c "
            SET application_name = '$PGAPPNAME';
            INSERT INTO analytics.events (event_type, user_id, payload)
            VALUES (
                (ARRAY['page_view','click','purchase','signup'])[1+(random()*3)::int],
                (random()*4+1)::int,
                jsonb_build_object('ts', now(), 'src', 'loadtest')
            );
        " > /dev/null
    ) &
    PIDS+=($!)
done

# ── 3. Slow analytical queries (show up as long-running) ─────
echo "[+] Starting analytical queries..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'analytics-worker';
        SELECT pg_sleep(0.5);
        SELECT event_type, count(*), avg(EXTRACT(EPOCH FROM created_at))
        FROM analytics.events
        GROUP BY event_type;
    " > /dev/null
    sleep 2
) &
PIDS+=($!)

(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'report-builder';
        SELECT pg_sleep(1);
        SELECT u.name, count(o.id), sum(o.total_cents)
        FROM app.users u
        JOIN app.orders o ON o.user_id = u.id
        JOIN app.order_items oi ON oi.order_id = o.id
        JOIN app.products p ON p.id = oi.product_id
        GROUP BY u.name;
    " > /dev/null
    sleep 3
) &
PIDS+=($!)

# ── 4. Really slow query (always visible in dashboard) ───────
echo "[+] Starting slow query generator..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'batch-processor';
        SELECT pg_sleep(5);
        SELECT path, count(*), avg(duration_ms)::int
        FROM analytics.page_views
        GROUP BY path
        ORDER BY count(*) DESC;
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

# ── 5. Idle-in-transaction (the bad pattern!) ─────────────────
echo "[+] Starting idle-in-transaction connections (2)..."
for i in 1 2; do
    (
        psql "$DB" -c "
            SET application_name = 'leaky-service-$i';
            BEGIN;
            SELECT * FROM app.users LIMIT 1;
            SELECT pg_sleep($DURATION);
            ROLLBACK;
        " > /dev/null 2>&1 || true
    ) &
    PIDS+=($!)
done

# ── 6. Lock contention ───────────────────────────────────────
echo "[+] Starting lock contention scenario..."
(
    # Holder: locks a row for a while
    run_until "$END" psql "$DB" -c "
        SET application_name = 'lock-holder';
        BEGIN;
        UPDATE app.products SET inventory = inventory WHERE id = 1;
        SELECT pg_sleep(3);
        COMMIT;
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

(
    sleep 1  # let the holder grab the lock first
    # Waiter: tries to update the same row
    run_until "$END" psql "$DB" -c "
        SET application_name = 'lock-waiter';
        UPDATE app.products SET inventory = inventory WHERE id = 1;
    " > /dev/null
    sleep 1
) &
PIDS+=($!)

# ── 7. Dead tuple generator (for vacuum stats) ───────────────
echo "[+] Starting dead tuple churn..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'data-churn';
        UPDATE analytics.page_views
        SET duration_ms = duration_ms + 1
        WHERE id IN (
            SELECT id FROM analytics.page_views
            ORDER BY random() LIMIT 500
        );
    " > /dev/null
    sleep 2
) &
PIDS+=($!)

# ── 8. SQLcommentor-style queries (for future parsing) ───────
echo "[+] Starting sqlcommentor queries..."
(
    run_until "$END" psql "$DB" -c "
        SET application_name = 'web-api';
        SELECT * FROM app.users WHERE id = 1
        /*app='user-service',route='/api/v1/users',controller='UserController',action='show'*/;
        SELECT pg_sleep(0.3);
    " > /dev/null
) &
PIDS+=($!)

echo ""
echo "All generators running. Watch with: make run"
echo "Load will stop in ${DURATION}s or press Ctrl+C"
echo ""

wait
