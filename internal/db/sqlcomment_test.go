package db_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/db"
)

var _ = Describe("ParseSQLComment", func() {
	It("extracts all known keys from a trailing block comment", func() {
		query := `SELECT * FROM users /* app='myapp',route='/users',controller='UsersController',action='index',framework='rails' */`
		c := db.ParseSQLComment(query)
		Expect(c.App).To(Equal("myapp"))
		Expect(c.Route).To(Equal("/users"))
		Expect(c.Controller).To(Equal("UsersController"))
		Expect(c.Action).To(Equal("index"))
		Expect(c.Framework).To(Equal("rails"))
	})

	It("returns an empty comment when there is no block comment", func() {
		c := db.ParseSQLComment("SELECT 1")
		Expect(c).To(Equal(db.SQLComment{}))
	})

	It("returns an empty comment for an empty string", func() {
		c := db.ParseSQLComment("")
		Expect(c).To(Equal(db.SQLComment{}))
	})

	It("handles a subset of keys", func() {
		query := `SELECT 1 /* app='svc',route='/health' */`
		c := db.ParseSQLComment(query)
		Expect(c.App).To(Equal("svc"))
		Expect(c.Route).To(Equal("/health"))
		Expect(c.Controller).To(BeEmpty())
		Expect(c.Action).To(BeEmpty())
		Expect(c.Framework).To(BeEmpty())
	})

	It("ignores unknown keys", func() {
		query := `SELECT 1 /* unknown='val',app='x' */`
		c := db.ParseSQLComment(query)
		Expect(c.App).To(Equal("x"))
	})

	It("handles empty values", func() {
		query := `SELECT 1 /* app='',route='' */`
		c := db.ParseSQLComment(query)
		Expect(c.App).To(BeEmpty())
		Expect(c.Route).To(BeEmpty())
	})

	It("only matches a trailing block comment", func() {
		query := `SELECT /* app='inner' */ 1 FROM t`
		c := db.ParseSQLComment(query)
		// The regex requires the comment at the end of the string
		Expect(c).To(Equal(db.SQLComment{}))
	})
})
