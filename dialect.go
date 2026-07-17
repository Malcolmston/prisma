package prisma

import (
	"strconv"
	"strings"
)

// UpsertStyle selects the SQL syntax a dialect uses for upserts and
// skip-duplicate inserts. The two dominant families are supported.
type UpsertStyle int

const (
	// OnConflict is the PostgreSQL/SQLite family:
	// "INSERT ... ON CONFLICT (cols) DO UPDATE SET ..." and
	// "INSERT ... ON CONFLICT DO NOTHING".
	OnConflict UpsertStyle = iota
	// OnDuplicateKey is the MySQL family:
	// "INSERT ... ON DUPLICATE KEY UPDATE ..." and "INSERT IGNORE INTO ...".
	OnDuplicateKey
)

// Dialect controls SQL syntax that varies between database engines: placeholder
// rendering, DISTINCT ON support, NULLS ordering, JSON extraction and upsert
// syntax. The three built-in dialects ([Question], [Dollar], [MySQLDialect])
// cover SQLite, PostgreSQL and MySQL respectively.
type Dialect struct {
	// Name identifies the dialect (for debugging).
	Name string
	// Placeholder renders the bind marker for the n-th parameter (1-based).
	Placeholder func(n int) string
	// DistinctOn reports whether the engine supports "SELECT DISTINCT ON (cols)"
	// (PostgreSQL). When false, [Query.Distinct] falls back to a plain
	// "SELECT DISTINCT".
	DistinctOn bool
	// NativeNulls reports whether the engine supports "NULLS FIRST"/"NULLS LAST"
	// ordering syntax (PostgreSQL). When false it is emulated with a leading
	// "column IS NULL" sort term.
	NativeNulls bool
	// JSONExtract renders an expression extracting the value at path from a JSON
	// column. If nil, the [Question] default (json_extract) is used.
	JSONExtract func(column string, path []string) string
	// Upsert selects the upsert / skip-duplicate SQL family.
	Upsert UpsertStyle
}

func question(int) string { return "?" }

// jsonExtractSQLite renders SQLite/MySQL-style json_extract(col, '$.a.b').
func jsonExtractSQLite(column string, path []string) string {
	var b strings.Builder
	b.WriteString("json_extract(")
	b.WriteString(column)
	b.WriteString(", '$")
	for _, p := range path {
		b.WriteString(".")
		b.WriteString(p)
	}
	b.WriteString("')")
	return b.String()
}

// jsonExtractPostgres renders PostgreSQL-style (col #>> '{a,b}').
func jsonExtractPostgres(column string, path []string) string {
	return "(" + column + " #>> '{" + strings.Join(path, ",") + "}')"
}

// Question is the default dialect using "?" placeholders (MySQL, SQLite, and
// the standard database/sql convention for many drivers). It uses ON CONFLICT
// upsert syntax (SQLite-compatible) and json_extract for JSON paths.
var Question = Dialect{
	Name:        "question",
	Placeholder: question,
	DistinctOn:  false,
	NativeNulls: false,
	JSONExtract: jsonExtractSQLite,
	Upsert:      OnConflict,
}

// Dollar uses "$1", "$2", ... placeholders (PostgreSQL, pq/pgx). It supports
// DISTINCT ON, native NULLS FIRST/LAST ordering and #>> JSON extraction.
var Dollar = Dialect{
	Name:        "dollar",
	Placeholder: func(n int) string { return "$" + strconv.Itoa(n) },
	DistinctOn:  true,
	NativeNulls: true,
	JSONExtract: jsonExtractPostgres,
	Upsert:      OnConflict,
}

// MySQLDialect uses "?" placeholders with MySQL's "ON DUPLICATE KEY UPDATE"
// upsert syntax and "INSERT IGNORE" for skip-duplicate inserts.
var MySQLDialect = Dialect{
	Name:        "mysql",
	Placeholder: question,
	DistinctOn:  false,
	NativeNulls: false,
	JSONExtract: jsonExtractSQLite,
	Upsert:      OnDuplicateKey,
}

// extractJSON dispatches to the dialect's JSONExtract (or the default).
func (d Dialect) extractJSON(column string, path []string) string {
	if d.JSONExtract != nil {
		return d.JSONExtract(column, path)
	}
	return jsonExtractSQLite(column, path)
}

// sqlWriter accumulates bound arguments and hands out placeholders in order. It
// also carries the registry and the current base model so that relation-filter
// conditions can resolve their target tables at compile time, and an err field
// that records the first resolution failure encountered while compiling.
type sqlWriter struct {
	d    Dialect
	args []any
	reg  *Registry
	base *Model
	err  error
}

// bind records a value and returns its placeholder.
func (w *sqlWriter) bind(v any) string {
	w.args = append(w.args, v)
	return w.d.Placeholder(len(w.args))
}

// fail records the first compile-time error; subsequent calls are ignored.
func (w *sqlWriter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}
