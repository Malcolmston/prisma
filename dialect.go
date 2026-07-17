package prisma

import "strconv"

// Dialect controls SQL syntax that varies between database engines. Currently
// this is limited to placeholder rendering, which is enough to target the two
// dominant styles.
type Dialect struct {
	// Name identifies the dialect (for debugging).
	Name string
	// Placeholder renders the bind marker for the n-th parameter (1-based).
	Placeholder func(n int) string
}

// Question is the default dialect using "?" placeholders (MySQL, SQLite, and
// the standard database/sql convention for many drivers).
var Question = Dialect{
	Name:        "question",
	Placeholder: func(int) string { return "?" },
}

// Dollar uses "$1", "$2", ... placeholders (PostgreSQL, pq/pgx).
var Dollar = Dialect{
	Name:        "dollar",
	Placeholder: func(n int) string { return "$" + strconv.Itoa(n) },
}

// sqlWriter accumulates bound arguments and hands out placeholders in order.
type sqlWriter struct {
	d    Dialect
	args []any
}

// bind records a value and returns its placeholder.
func (w *sqlWriter) bind(v any) string {
	w.args = append(w.args, v)
	return w.d.Placeholder(len(w.args))
}
