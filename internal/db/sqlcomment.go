package db

import "regexp"

// SQLComment holds metadata extracted from a SQLcommentor-style comment
// appended to a query string.
type SQLComment struct {
	App        string
	Route      string
	Controller string
	Action     string
	Framework  string
}

var sqlCommentBlockRe = regexp.MustCompile(`/\*(.+?)\*/\s*$`)
var sqlCommentKVRe = regexp.MustCompile(`(\w+)='([^']*)'`)

// ParseSQLComment extracts SQLcommentor key-value pairs from the trailing
// block comment of a SQL query. Returns an empty SQLComment if no comment
// is found.
func ParseSQLComment(query string) SQLComment {
	block := sqlCommentBlockRe.FindStringSubmatch(query)
	if block == nil {
		return SQLComment{}
	}

	var c SQLComment
	for _, m := range sqlCommentKVRe.FindAllStringSubmatch(block[1], -1) {
		switch m[1] {
		case "app":
			c.App = m[2]
		case "route":
			c.Route = m[2]
		case "controller":
			c.Controller = m[2]
		case "action":
			c.Action = m[2]
		case "framework":
			c.Framework = m[2]
		}
	}
	return c
}
