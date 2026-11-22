package db_test

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/knownbug"
	"github.com/fraser-isbester/tusk/internal/pgtest"
)

// These specs run every query tusk issues against a real server, with real
// client sessions producing the activity, transactions, and lock contention the
// views and rules engine depend on. Catalog SQL is the part of tusk most likely
// to regress silently (column order vs. Scan, version-specific columns, join
// predicates), and none of it is exercised by the unit tests.
var _ = Describe("DB against PostgreSQL", Label("integration"), Ordered, ContinueOnFailure, func() {
	var (
		ctx      context.Context
		tmp      *pgtest.TempDB
		database *db.DB
	)

	// client opens an application session tagged with a unique app name so
	// assertions on cluster-wide views never match another test's backends.
	client := func(app string) (*pgx.Conn, int) {
		conn, err := pgtest.Client(ctx, tmp.DSN, app)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = conn.Close(context.Background()) })
		return conn, int(conn.PgConn().PID())
	}

	exec := func(conn *pgx.Conn, sql string) {
		GinkgoHelper()
		_, err := conn.Exec(ctx, sql)
		Expect(err).NotTo(HaveOccurred())
	}

	activeQueries := func() ([]db.Query, error) { return database.GetActiveQueries(ctx) }
	locks := func() ([]db.Lock, error) { return database.GetLocks(ctx) }
	transactions := func() ([]db.Transaction, error) { return database.GetTransactions(ctx) }

	queryFor := func(pid int) db.Query {
		GinkgoHelper()
		qs, err := database.GetActiveQueries(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, q := range qs {
			if q.PID == pid {
				return q
			}
		}
		Fail("pid not found in active queries")
		return db.Query{}
	}

	BeforeAll(func() {
		ctx = context.Background()
		dsn, required := pgtest.Resolve()
		if dsn == "" {
			if required {
				Fail(pgtest.EnvDSN + " must be set when " + pgtest.EnvRequire + " is set")
			}
			Skip(pgtest.EnvDSN + " not set")
		}

		var err error
		tmp, err = pgtest.CreateDatabase(ctx, dsn)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(tmp.Drop(context.Background())).To(Succeed()) })

		database, err = db.New(tmp.DSN)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(database.Close)

		// A short-lived session: a backend flushes its pending cumulative stats
		// (n_live_tup etc.) on exit, but only every ~10s while it sits idle.
		setup, err := pgtest.Client(ctx, tmp.DSN, "tusk_test_setup")
		Expect(err).NotTo(HaveOccurred())
		exec(setup, `
			CREATE TABLE accounts (id int PRIMARY KEY, balance int NOT NULL DEFAULT 0, note text);
			INSERT INTO accounts SELECT g, 100 FROM generate_series(1, 10) g;

			CREATE TABLE big (id int PRIMARY KEY, pad text);
			INSERT INTO big SELECT g, repeat('x', 200) FROM generate_series(1, 5000) g;

			-- Quoted, mixed-case, space-containing identifiers in a non-public
			-- schema: GetTableDetail builds a regclass from these.
			CREATE SCHEMA "Billing";
			CREATE TABLE "Billing"."Line Items" (
				order_id int,
				line_no  int,
				sku      text NOT NULL,
				qty      int DEFAULT 1,
				PRIMARY KEY (order_id, line_no)
			);
			CREATE INDEX line_items_sku_idx ON "Billing"."Line Items" (sku);
		`)
		Expect(setup.Close(ctx)).To(Succeed())
	})

	Describe("New", func() {
		It("rejects an unparseable connection string", func() {
			_, err := db.New("postgres://%zz")
			Expect(err).To(MatchError(ContainSubstring("parsing connection string")))
		})

		It("identifies itself and bounds every statement with a timeout", func() {
			var app, timeout string
			Expect(database.Pool().QueryRow(ctx,
				"SELECT current_setting('application_name'), current_setting('statement_timeout')",
			).Scan(&app, &timeout)).To(Succeed())
			Expect(app).To(Equal("tusk"))
			Expect(timeout).To(Equal("5s"))
		})
	})

	Describe("GetServerInfo", func() {
		It("reports version, a positive uptime, and max_connections", func() {
			info, err := database.GetServerInfo(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Version).To(HavePrefix("PostgreSQL"))
			Expect(info.Uptime).To(BeNumerically(">", 0))

			var maxConns int
			Expect(database.Pool().QueryRow(ctx, "SELECT current_setting('max_connections')::int").Scan(&maxConns)).To(Succeed())
			Expect(info.MaxConnections).To(Equal(maxConns))
		})
	})

	Describe("GetDatabaseStats", func() {
		It("returns counters for the current database with a ratio in [0,1]", func() {
			before, err := database.GetDatabaseStats(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(before.CacheHitRatio).To(And(BeNumerically(">=", 0), BeNumerically("<=", 1)))

			c, _ := client(pgtest.UniqueName("stats"))
			exec(c, "BEGIN; SELECT count(*) FROM big; COMMIT")
			Expect(c.Close(ctx)).To(Succeed()) // exit flushes pending stats

			// Cumulative stats are flushed asynchronously (up to ~1s idle).
			Eventually(func() (int64, error) {
				s, err := database.GetDatabaseStats(ctx)
				if err != nil {
					return 0, err
				}
				return s.XactCommit, nil
			}).WithTimeout(10 * time.Second).Should(BeNumerically(">", before.XactCommit))
		})
	})

	Describe("GetActiveQueries", func() {
		It("reports a running query with timing, wait event, and sqlcommenter tags", func() {
			app := pgtest.UniqueName("aq")
			c, pid := client(app)
			done := pgtest.Background(ctx, c,
				"SELECT pg_sleep(30) /*action='charge',controller='payments',framework='rails',route='/payments/charge'*/")
			DeferCleanup(func() { _ = database.CancelQuery(context.Background(), pid); <-done })

			Eventually(activeQueries).WithTimeout(10 * time.Second).Should(ContainElement(And(
				HaveField("PID", pid),
				HaveField("State", "active"),
				HaveField("WaitEvent", "PgSleep"),
			)))

			Eventually(func() time.Duration { return queryFor(pid).Duration }).
				WithTimeout(5 * time.Second).Should(BeNumerically(">", 100*time.Millisecond))

			q := queryFor(pid)
			Expect(q.App).To(Equal(app))
			Expect(q.Database).To(Equal(tmp.Name))
			Expect(q.BackendStart).NotTo(BeZero())
			Expect(q.QueryStart).To(BeTemporally(">=", q.BackendStart))
			Expect(q.QueryText).To(ContainSubstring("pg_sleep(30)"))
			Expect(q.BlockedBy).To(BeZero())
			Expect(q.Comment).To(Equal(db.SQLComment{
				Route: "/payments/charge", Controller: "payments", Action: "charge", Framework: "rails",
			}))
		})

		It("excludes the backend running the monitoring query itself", func() {
			qs, err := database.GetActiveQueries(ctx)
			Expect(err).NotTo(HaveOccurred())
			// GetActiveQueries is cluster-wide, and the rules and tuskd suites run
			// their own monitoring concurrently against their own databases. In
			// this suite's database, our backend is the only one running tusk's SQL.
			Expect(qs).NotTo(ContainElement(And(
				HaveField("Database", tmp.Name),
				HaveField("State", "active"),
				HaveField("QueryText", ContainSubstring("AS blocked_by")),
			)))
		})

		It("orders backends by query duration, longest first", func() {
			qs, err := database.GetActiveQueries(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sort.SliceIsSorted(qs, func(i, j int) bool { return qs[i].Duration > qs[j].Duration })).To(BeTrue())
		})
	})

	Describe("lock contention on a table", func() {
		It("pairs the waiter with its blocker in GetLocks, GetActiveQueries, and GetTransactions", func() {
			holder, holderPID := client(pgtest.UniqueName("holder"))
			waiterApp := pgtest.UniqueName("waiter")
			waiter, waiterPID := client(waiterApp)

			exec(holder, "BEGIN; LOCK TABLE accounts IN ACCESS EXCLUSIVE MODE")
			done := pgtest.Background(ctx, waiter, "SELECT count(*) FROM accounts")

			Eventually(locks).WithTimeout(10 * time.Second).Should(ContainElement(And(
				HaveField("BlockedPID", waiterPID),
				HaveField("BlockingPID", holderPID),
				HaveField("BlockedApp", waiterApp),
				HaveField("LockType", "relation"),
				HaveField("Mode", "AccessShareLock"),
				HaveField("BlockedQuery", ContainSubstring("FROM accounts")),
				HaveField("BlockingQuery", ContainSubstring("LOCK TABLE")),
			)))

			w := queryFor(waiterPID)
			Expect(w.BlockedBy).To(Equal(holderPID))
			Expect(w.WaitEventType).To(Equal("Lock"))

			txns, err := database.GetTransactions(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(txns).To(ContainElement(And(
				HaveField("PID", holderPID),
				HaveField("State", "idle in transaction"),
				HaveField("LockCount", BeNumerically(">=", 1)),
			)))

			exec(holder, "ROLLBACK")
			Eventually(done).WithTimeout(5 * time.Second).Should(Receive(BeNil()))
			Eventually(locks).Should(Not(ContainElement(HaveField("BlockedPID", waiterPID))))
		})
	})

	Describe("lock contention on a row", func() {
		// Row-lock waits are on a transactionid lock. queryLocks and the
		// blocked_by subquery only compare database/relation/page/tuple, which
		// are all NULL for transactionid locks, and never compare the
		// transactionid itself or require the blocker's lock to be granted.
		// So every other open writing transaction is reported as a blocker.
		It("attributes the wait only to the transaction holding the row", func() {

			holder, holderPID := client(pgtest.UniqueName("rowholder"))
			bystander, bystanderPID := client(pgtest.UniqueName("bystander"))
			waiter, waiterPID := client(pgtest.UniqueName("rowwaiter"))

			exec(holder, "BEGIN; UPDATE accounts SET balance = balance + 1 WHERE id = 1")
			// An unrelated writer: holds its own transactionid lock, blocks nobody.
			exec(bystander, "BEGIN; UPDATE accounts SET balance = balance + 1 WHERE id = 2")
			done := pgtest.Background(ctx, waiter, "UPDATE accounts SET balance = balance + 1 WHERE id = 1")
			DeferCleanup(func() {
				_, _ = holder.Exec(context.Background(), "ROLLBACK")
				_, _ = bystander.Exec(context.Background(), "ROLLBACK")
				<-done
			})

			Eventually(locks).WithTimeout(10 * time.Second).Should(ContainElement(And(
				HaveField("BlockedPID", waiterPID),
				HaveField("BlockingPID", holderPID),
			)))
			Expect(locks()).NotTo(ContainElement(And(
				HaveField("BlockedPID", waiterPID),
				HaveField("BlockingPID", bystanderPID),
			)))
			Expect(queryFor(waiterPID).BlockedBy).To(Equal(holderPID))
		})
	})

	Describe("GetTransactions", func() {
		It("reports an idle-in-transaction session with its age and start times", func() {
			c, pid := client(pgtest.UniqueName("idletx"))
			exec(c, "BEGIN; SELECT * FROM accounts WHERE id = 3 FOR UPDATE")

			Eventually(transactions).WithTimeout(5 * time.Second).Should(ContainElement(And(
				HaveField("PID", pid),
				HaveField("State", "idle in transaction"),
				HaveField("QueryText", ContainSubstring("FOR UPDATE")),
				HaveField("LockCount", BeNumerically(">=", 1)),
				HaveField("XactDuration", BeNumerically(">", 0)),
			)))

			txns, err := database.GetTransactions(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sort.SliceIsSorted(txns, func(i, j int) bool { return txns[i].XactStart.Before(txns[j].XactStart) })).To(BeTrue())
			for _, t := range txns {
				if t.PID == pid {
					Expect(t.BackendStart).NotTo(BeZero())
					Expect(t.XactStart).To(BeTemporally(">=", t.BackendStart))
				}
			}

			exec(c, "COMMIT")
			Eventually(transactions).Should(Not(ContainElement(HaveField("PID", pid))))
		})
	})

	Describe("CancelQuery and TerminateBackend", func() {
		It("cancels the running statement but leaves the session usable", func() {
			c, pid := client(pgtest.UniqueName("cancel"))
			done := pgtest.Background(ctx, c, "SELECT pg_sleep(30)")
			Eventually(activeQueries).WithTimeout(5 * time.Second).Should(ContainElement(And(
				HaveField("PID", pid), HaveField("WaitEvent", "PgSleep"),
			)))

			Expect(database.CancelQuery(ctx, pid)).To(Succeed())

			var err error
			Eventually(done).WithTimeout(5 * time.Second).Should(Receive(&err))
			var pgErr *pgconn.PgError
			Expect(errors.As(err, &pgErr)).To(BeTrue())
			Expect(pgErr.Code).To(Equal("57014")) // query_canceled

			var one int
			Expect(c.QueryRow(ctx, "SELECT 1").Scan(&one)).To(Succeed())
		})

		It("terminates the backend so it disappears from pg_stat_activity", func() {
			c, pid := client(pgtest.UniqueName("terminate"))
			Eventually(activeQueries).Should(ContainElement(HaveField("PID", pid)))

			Expect(database.TerminateBackend(ctx, pid)).To(Succeed())

			Eventually(activeQueries).WithTimeout(5 * time.Second).Should(Not(ContainElement(HaveField("PID", pid))))
			_, err := c.Exec(ctx, "SELECT 1")
			Expect(err).To(HaveOccurred())
		})

		It("reports an error for a PID that is not a backend", func() {
			const bogus = 2147483000
			Expect(database.CancelQuery(ctx, bogus)).To(MatchError(ContainSubstring("returned false")))
			Expect(database.TerminateBackend(ctx, bogus)).To(MatchError(ContainSubstring("returned false")))
		})
	})

	Describe("GetConnections", func() {
		It("groups sessions by user, application, and state", func() {
			app := pgtest.UniqueName("pool")
			for range 3 {
				client(app)
			}
			Eventually(func() ([]db.ConnectionGroup, error) { return database.GetConnections(ctx) }).
				Should(ContainElement(And(
					HaveField("App", app),
					HaveField("State", "idle"),
					HaveField("Count", 3),
				)))
		})
	})

	Describe("GetDatabases", func() {
		It("lists non-template databases with size and owner", func() {
			dbs, err := database.GetDatabases(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(dbs).To(ContainElement(And(
				HaveField("Name", tmp.Name),
				HaveField("Size", BeNumerically(">", 0)),
				HaveField("Owner", Not(BeEmpty())),
			)))
			Expect(dbs).NotTo(ContainElement(HaveField("Name", HavePrefix("template"))))
			Expect(sort.SliceIsSorted(dbs, func(i, j int) bool { return dbs[i].Size > dbs[j].Size })).To(BeTrue())
		})
	})

	Describe("GetRoles", func() {
		It("maps role attributes and sorts by name", func() {
			role := pgtest.UniqueName("tusk_role")
			setup, _ := client("tusk_test_setup")
			exec(setup, "CREATE ROLE "+role+" NOLOGIN CREATEDB CONNECTION LIMIT 7")
			DeferCleanup(func() { _, _ = setup.Exec(context.Background(), "DROP ROLE IF EXISTS "+role) })

			roles, err := database.GetRoles(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(roles).To(ContainElement(db.RoleInfo{
				Name: role, CanCreateDB: true, ConnLimit: 7,
			}))
			Expect(sort.SliceIsSorted(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })).To(BeTrue())
		})
	})

	Describe("GetTables", func() {
		It("lists user tables largest first with tuple counts", func() {
			tables, err := database.GetTables(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tables).To(ContainElement(And(HaveField("Schema", "Billing"), HaveField("Name", "Line Items"))))
			Expect(sort.SliceIsSorted(tables, func(i, j int) bool { return tables[i].TotalSize > tables[j].TotalSize })).To(BeTrue())

			idx := func(name string) int {
				for i, t := range tables {
					if t.Schema == "public" && t.Name == name {
						return i
					}
				}
				return -1
			}
			Expect(idx("big")).To(BeNumerically(">=", 0))
			Expect(idx("big")).To(BeNumerically("<", idx("accounts")))

			Eventually(func() ([]db.TableInfo, error) { return database.GetTables(ctx) }).
				WithTimeout(10 * time.Second).Should(ContainElement(And(
				HaveField("Name", "big"),
				HaveField("LiveTuples", int64(5000)),
			)))
		})
	})

	Describe("GetIndexes", func() {
		It("lists user indexes with their table, ordered least-used first", func() {
			idxs, err := database.GetIndexes(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(idxs).To(ContainElement(And(
				HaveField("Schema", "Billing"),
				HaveField("Table", "Line Items"),
				HaveField("IndexName", "line_items_sku_idx"),
				HaveField("Size", BeNumerically(">", 0)),
			)))
			Expect(sort.SliceIsSorted(idxs, func(i, j int) bool { return idxs[i].Scans < idxs[j].Scans })).To(BeTrue())
		})
	})

	Describe("tables without any index", func() {
		// pg_stat_user_tables.idx_scan is NULL for a table with no indexes, and
		// both queries scan it into an int64. One index-less table anywhere in
		// the database makes GetTables fail outright (the :tables view renders
		// nothing), and GetTableDetail fails for that table.
		It("are listed and described", func() {
			knownbug.Skip("idx_scan is NULL for index-less tables; GetTables/GetTableDetail scan it into int64 and error")

			setup, _ := client("tusk_test_setup")
			exec(setup, "CREATE TABLE heap_only (x int)")
			DeferCleanup(func() { _, _ = setup.Exec(context.Background(), "DROP TABLE IF EXISTS heap_only") })

			tables, err := database.GetTables(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tables).To(ContainElement(And(HaveField("Name", "heap_only"), HaveField("IdxScan", int64(0)))))

			td, err := database.GetTableDetail(ctx, "public", "heap_only")
			Expect(err).NotTo(HaveOccurred())
			Expect(td.Indexes).To(BeEmpty())
		})
	})

	Describe("GetTableDetail", func() {
		It("describes columns, primary key, and indexes of a quoted mixed-case table", func() {
			td, err := database.GetTableDetail(ctx, "Billing", "Line Items")
			Expect(err).NotTo(HaveOccurred())
			Expect(td.Schema).To(Equal("Billing"))
			Expect(td.Name).To(Equal("Line Items"))
			Expect(td.Columns).To(Equal([]db.ColumnInfo{
				{Name: "order_id", DataType: "integer", Nullable: false, IsPrimary: true},
				{Name: "line_no", DataType: "integer", Nullable: false, IsPrimary: true},
				{Name: "sku", DataType: "text", Nullable: false},
				{Name: "qty", DataType: "integer", Nullable: true, Default: "1"},
			}))

			names := make([]string, 0, len(td.Indexes))
			for _, i := range td.Indexes {
				Expect(i.Schema).To(Equal("Billing"))
				Expect(i.Table).To(Equal("Line Items"))
				names = append(names, i.IndexName)
			}
			Expect(names).To(ConsistOf("Line Items_pkey", "line_items_sku_idx"))
		})

		It("returns an error for a table that does not exist", func() {
			_, err := database.GetTableDetail(ctx, "public", "nope")
			Expect(err).To(MatchError(ContainSubstring("public.nope")))
		})
	})

	Describe("GetSlowQueries", func() {
		It("returns an empty, non-nil slice when pg_stat_statements is not installed", func() {
			qs, err := database.GetSlowQueries(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(qs).NotTo(BeNil())
			Expect(qs).To(BeEmpty())
		})

		It("reads pg_stat_statements when the extension is installed", func() {
			var preload string
			Expect(database.Pool().QueryRow(ctx, "SELECT current_setting('shared_preload_libraries')").Scan(&preload)).To(Succeed())
			if !slices.Contains(strings.Split(strings.ReplaceAll(preload, " ", ""), ","), "pg_stat_statements") {
				Skip("server not started with shared_preload_libraries=pg_stat_statements")
			}

			setup, _ := client("tusk_test_setup")
			exec(setup, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
				CREATE TABLE slow_marker (id int PRIMARY KEY); INSERT INTO slow_marker VALUES (1)`)
			DeferCleanup(func() {
				_, _ = setup.Exec(context.Background(), "DROP EXTENSION IF EXISTS pg_stat_statements; DROP TABLE IF EXISTS slow_marker")
			})

			c, _ := client(pgtest.UniqueName("slow"))
			// GetSlowQueries ranks cluster-wide, so the marker has to earn a top-50
			// spot on its own (1.5s total). Deliberately no pg_stat_statements_reset():
			// unfiltered it wipes every database's history on a shared server, and
			// scoped to this fresh database it would be a no-op.
			for range 3 {
				exec(c, "SELECT pg_sleep(0.5) FROM slow_marker")
			}

			Eventually(func() ([]db.SlowQuery, error) { return database.GetSlowQueries(ctx) }).
				WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(ContainElement(And(
				HaveField("Query", ContainSubstring("slow_marker")),
				HaveField("Calls", BeNumerically(">=", 3)),
				HaveField("QueryID", Not(BeZero())),
				HaveField("HitRatio", And(BeNumerically(">=", 0), BeNumerically("<=", 1))),
			)))
		})
	})
})
