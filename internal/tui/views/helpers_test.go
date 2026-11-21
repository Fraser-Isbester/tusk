package views

import (
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rivo/tview"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
	"github.com/fraser-isbester/tusk/internal/knownbug"
	"github.com/fraser-isbester/tusk/internal/rules"
)

var _ = Describe("formatDuration", func() {
	DescribeTable("picks a unit by magnitude",
		func(d time.Duration, want string) { Expect(formatDuration(d)).To(Equal(want)) },
		Entry("sub-second", 250*time.Millisecond, "250ms"),
		Entry("zero", time.Duration(0), "0ms"),
		Entry("seconds", 1500*time.Millisecond, "1.5s"),
		Entry("just under a minute", 59*time.Second, "59.0s"),
		Entry("minutes", 61*time.Second, "1m1s"),
		Entry("hours drop seconds", 2*time.Hour+3*time.Minute+59*time.Second, "2h3m"),
		Entry("days stay in hours", 50*time.Hour, "50h0m"),
	)
})

var _ = Describe("formatSize", func() {
	DescribeTable("uses binary units with one decimal",
		func(b int64, want string) { Expect(formatSize(b)).To(Equal(want)) },
		Entry("bytes", int64(1023), "1023 B"),
		Entry("KB boundary", int64(1024), "1.0 KB"),
		Entry("MB", int64(5*1024*1024+512*1024), "5.5 MB"),
		Entry("GB boundary", int64(1<<30), "1.0 GB"),
		Entry("TB", int64(3<<40), "3.0 TB"),
	)
})

var _ = Describe("timeAgo", func() {
	It("renders a nil time as never", func() {
		Expect(timeAgo(nil)).To(Equal("never"))
	})

	// Entries carry offsets, not timestamps: Entry arguments are evaluated at
	// tree construction, so a time.Now() there would age by however long the
	// specs that run first take.
	DescribeTable("renders relative times",
		func(offset time.Duration, want string) {
			t := time.Now().Add(-offset)
			Expect(timeAgo(&t)).To(Equal(want))
		},
		Entry("seconds", 5*time.Second, "5s ago"),
		Entry("minutes", 3*time.Minute+5*time.Second, "3m ago"),
		Entry("hours", 5*time.Hour+time.Minute, "5h ago"),
		Entry("days", 49*time.Hour, "2d ago"),
	)
})

var _ = Describe("truncate", func() {
	DescribeTable("caps length, marking the cut with an ellipsis",
		func(s string, n int, want string) {
			got := truncate(s, n)
			Expect(got).To(Equal(want))
			Expect(len(got)).To(BeNumerically("<=", n))
		},
		Entry("fits", "SELECT 1", 8, "SELECT 1"),
		Entry("cut", "SELECT * FROM orders", 10, "SELECT ..."),
		Entry("tiny limit has no room for ellipsis", "SELECT", 3, "SEL"),
	)

	It("never splits a multi-byte character", func() {
		knownbug.Skip("truncate slices bytes, producing invalid UTF-8 for non-ASCII query text")
		for n := 4; n < 16; n++ {
			Expect(utf8.ValidString(truncate("SELECT 'café ünïcødé 日本語'", n))).To(BeTrue(), "n=%d", n)
		}
	})
})

var _ = Describe("countStatements", func() {
	DescribeTable("counts non-empty ;-separated statements, minimum one",
		func(q string, want int) { Expect(countStatements(q)).To(Equal(want)) },
		Entry("single", "SELECT 1", 1),
		Entry("trailing semicolon", "SELECT 1;", 1),
		Entry("multi", "BEGIN; UPDATE t SET x = 1; COMMIT;", 3),
		Entry("empty", "", 1),
		Entry("only separators", " ; ; ", 1),
	)
})

var _ = Describe("mergeComments", func() {
	It("merges sqlcommenter tags across statements, later statements winning", func() {
		q := "BEGIN /*route='/a',framework='rails'*/; UPDATE t SET x = 1 /*controller='orders',route='/b'*/; COMMIT"
		Expect(mergeComments(q)).To(Equal(db.SQLComment{Route: "/b", Controller: "orders", Framework: "rails"}))
	})
})

var _ = Describe("formatSQL", func() {
	It("breaks major clauses onto their own lines and indents conditions", func() {
		in := "SELECT a, b FROM t LEFT JOIN u ON t.id = u.id WHERE a = 1 AND b > 1 ORDER BY a LIMIT 5"
		Expect(formatSQL(in)).To(Equal(
			"SELECT a, b\nFROM t\nLEFT JOIN u\n  ON t.id = u.id\nWHERE a = 1\n  AND b > 1\nORDER BY a\nLIMIT 5"))
	})

	It("does not split inside string literals or identifiers containing keywords", func() {
		in := "select * from orders where note = 'select from where' and created_on > now()"
		Expect(formatSQL(in)).To(Equal(
			"select *\nFROM orders\nWHERE note = 'select from where'\n  AND created_on > now()"))
	})

	It("leaves keyword-free text untouched", func() {
		Expect(formatSQL("vacuum analyze t")).To(Equal("vacuum analyze t"))
	})

	It("only changes whitespace and keyword case", func() {
		for _, q := range sqlCorpus {
			norm := func(s string) string { return strings.ToUpper(strings.Join(strings.Fields(s), "")) }
			Expect(norm(formatSQL(q))).To(Equal(norm(q)), q)
		}
	})
})

var _ = Describe("highlightSQL", func() {
	It("colors keywords, literals, numbers, and comments with distinct tags", func() {
		Expect(highlightSQL("SELECT 'x' FROM t WHERE id = 42 -- note")).To(Equal(
			"[#5F87FF]SELECT[-] [#00D700]'x'[-] [#5F87FF]FROM[-] t [#5F87FF]WHERE[-] id = [#FFD700]42[-] [#585858]-- note[-]"))
	})

	It("does not color keywords or numbers inside literals and comments", func() {
		Expect(highlightSQL("'select 1' /* from 2 */")).To(Equal("[#00D700]'select 1'[-] [#585858]/* from 2 */[-]"))
	})

	It("never changes the visible text", func() {
		for _, q := range sqlCorpus { // corpus has no '[', so tview renders q itself verbatim
			Expect(tview.TaggedStringWidth(highlightSQL(q))).To(Equal(tview.TaggedStringWidth(q)), q)
		}
	})

	It("renders query text containing brackets verbatim", func() {
		// The output is fed to tview with dynamic colors on, but query text is
		// never tview.Escape'd: JSON/array literals like '["a"]' or '[red]' are
		// parsed as style tags and vanish from the query/lock detail panes.
		knownbug.Skip("query text is not tview-escaped; bracketed literals are swallowed as color tags")
		for _, q := range []string{`SELECT '["a","b"]'::jsonb`, "SELECT '[red]'", "SELECT arr[1:2]"} {
			Expect(tview.TaggedStringWidth(highlightSQL(q))).To(Equal(utf8.RuneCountInString(q)), q)
		}
	})
})

var _ = Describe("violationIcon", func() {
	v := func(action string, kind rules.EventKind) rules.Violation {
		return rules.Violation{ActionName: action, Events: []rules.ViolationEvent{{Kind: rules.EventDetected}, {Kind: kind}}}
	}
	DescribeTable("reflects the last lifecycle event",
		func(viol rules.Violation, want string) { Expect(violationIcon(viol)).To(Equal(want)) },
		Entry("detected only", rules.Violation{Events: []rules.ViolationEvent{{Kind: rules.EventDetected}}}, "[!]"),
		Entry("dry-run action", v("terminate", rules.EventAction), "[!]"),
		Entry("terminated", v("terminate", rules.EventSent), "[T]"),
		Entry("canceled", v("cancel", rules.EventSent), "[C]"),
		Entry("logged", v("log", rules.EventSent), "[L]"),
		Entry("error", v("cancel", rules.EventError), "[E]"),
		Entry("closed", v("cancel", rules.EventClosed), "[-]"),
	)
})

var _ = Describe("celVars", func() {
	// The rule form's autocomplete list is a hand-maintained copy of the CEL
	// environment; drift means users can't discover (or are offered invalid) fields.
	DescribeTable("offers exactly the variables the rules engine declares",
		func(rt rules.ResourceType) {
			env, err := rules.EnvForResource(rt)
			Expect(err).NotTo(HaveOccurred())
			declared := map[string]bool{}
			for _, v := range env.Variables() {
				declared[v.Name()] = true
			}
			Expect(celVars(string(rt))).To(ConsistOf(slices.Collect(maps.Keys(declared))))
		},
		Entry("query", rules.ResourceQuery),
		Entry("transaction", rules.ResourceTransaction),
		Entry("lock", rules.ResourceLock),
	)

	It("offers nothing for an unknown resource", func() {
		Expect(celVars("bogus")).To(BeNil())
	})
})

var _ = Describe("lastToken", func() {
	DescribeTable("returns the identifier being typed",
		func(in, want string) { Expect(lastToken(in)).To(Equal(want)) },
		Entry("bare", "dur", "dur"),
		Entry("after operator", "state == 'active' && dur", "dur"),
		Entry("after paren", "size(que", "que"),
		Entry("inside a string", "app == 'my", "my"),
		Entry("trailing space", "pid ", ""),
	)
})

var sqlCorpus = []string{
	"SELECT 1",
	"select * from orders where id = 42",
	"SELECT a, b FROM t LEFT JOIN u ON t.id = u.id WHERE a = 'x' AND b > 1 ORDER BY a DESC LIMIT 10",
	"UPDATE accounts SET balance = balance - 100 WHERE id = 7 RETURNING balance",
	"INSERT INTO t (a, b) VALUES (1, 'two') /*route='/x',controller='c'*/",
	"BEGIN; DELETE FROM sessions WHERE expires_at < now(); COMMIT",
	"WITH RECURSIVE r AS (SELECT 1 UNION SELECT n + 1 FROM r WHERE n < 5) SELECT * FROM r",
	"SELECT 'it''s', 'select from' -- trailing from where\n FROM t",
	"SELECT pg_sleep(30) /* action='charge' */",
	"SELECT name FROM users WHERE name LIKE 'é%' OR name IS NULL",
}
