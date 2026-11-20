package rules

import (
	"maps"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
)

// The CEL environments (what a rule may reference) and the activation builders
// (what the engine binds at eval time) are maintained by hand in two places. A
// variable declared but not bound type-checks at BuildRules time and then
// errors on every evaluation, which Evaluate swallows: the rule silently never
// fires. These specs pin the two together.
var _ = Describe("CEL environment / activation contract", func() {
	DescribeTable("binds exactly the declared variables, each with its declared type",
		func(rt ResourceType, activation map[string]any) {
			env, err := EnvForResource(rt)
			Expect(err).NotTo(HaveOccurred())

			declared := map[string]string{}
			for _, v := range env.Variables() {
				declared[v.Name()] = v.Type().TypeName()
			}
			Expect(slices.Sorted(maps.Keys(activation))).To(Equal(slices.Sorted(maps.Keys(declared))))

			for name, typeName := range declared {
				ast, iss := env.Compile(name)
				Expect(iss.Err()).NotTo(HaveOccurred())
				prg, err := env.Program(ast)
				Expect(err).NotTo(HaveOccurred())
				out, _, err := prg.Eval(activation)
				Expect(err).NotTo(HaveOccurred(), "evaluating %q", name)
				Expect(out.Type().TypeName()).To(Equal(typeName), "type of %q", name)
			}
		},
		Entry("query", ResourceQuery, queryActivation(db.Query{})),
		Entry("transaction", ResourceTransaction, transactionActivation(db.Transaction{})),
		Entry("lock", ResourceLock, lockActivation(db.Lock{})),
	)

	It("maps query struct fields onto the variables users write rules against", func() {
		q := db.Query{
			ResourceBase:  db.ResourceBase{PID: 7, User: "u", App: "a", Database: "d", ClientAddr: "10.0.0.1", State: "active"},
			Duration:      3 * time.Second,
			WaitEventType: "Lock",
			WaitEvent:     "transactionid",
			QueryText:     "SELECT 1",
			Comment:       db.SQLComment{Route: "/r", Controller: "c", Action: "act", Framework: "f"},
			BlockedBy:     9,
			QueryID:       -42,
		}
		Expect(queryActivation(q)).To(Equal(map[string]any{
			"pid": int64(7), "user": "u", "app": "a", "database": "d", "client_addr": "10.0.0.1",
			"state": "active", "duration": 3 * time.Second, "wait_event_type": "Lock",
			"wait_event": "transactionid", "query": "SELECT 1", "route": "/r", "controller": "c",
			"action_name": "act", "framework": "f", "blocked_by": int64(9), "query_id": int64(-42),
		}))
	})
})
