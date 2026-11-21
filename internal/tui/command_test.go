package tui

import (
	"github.com/rivo/tview"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
)

var _ = Describe("CommandRegistry.Match", func() {
	r := NewCommandRegistry()

	DescribeTable("resolves a unique prefix of a name or alias to its view",
		func(in, view string) {
			got, ok := r.Match(in)
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal(view))
		},
		Entry("full name", "queries", "queries"),
		Entry("alias", "q", "queries"),
		Entry("several aliases of one command are not ambiguous", "tx", "transactions"),
		Entry("prefix", "tab", "tables"),
		Entry("trimmed and case-insensitive", "  LOCKS ", "locks"),
		Entry("index alias", "idx", "indexes"),
		Entry("sessions alias", "conn", "sessions"),
		Entry("rules vs roles", "ru", "rules"),
		Entry("violations", "v", "violations"),
	)

	DescribeTable("rejects ambiguous, empty, and unknown input",
		func(in string) {
			got, ok := r.Match(in)
			Expect(ok).To(BeFalse())
			Expect(got).To(BeEmpty())
		},
		Entry("t: tables or transactions", "t"),
		Entry("empty", ""),
		Entry("whitespace", "   "),
		Entry("unknown", "dashboard"),
		Entry("longer than any name", "queriesx"),
	)

	It("maps every command to a view the app registers", func() {
		// A typo in a ViewName silently breaks :command navigation.
		a := &App{app: tview.NewApplication(), viewMap: map[string]View{}, queryHistory: db.NewQueryHistory(1)}
		a.buildLayout()
		a.registerViews() // constructors only; nothing is started or queried
		for _, c := range r.commands {
			Expect(a.viewMap).To(HaveKey(c.ViewName), c.Name)
		}
	})
})
