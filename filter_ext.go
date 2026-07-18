package prisma

import (
	"fmt"
	"strings"
)

// nullCond compiles to "column IS [NOT] NULL".
type nullCond struct {
	column string
	negate bool
}

func (c nullCond) compile(*sqlWriter) string {
	if c.negate {
		return c.column + " IS NOT NULL"
	}
	return c.column + " IS NULL"
}

// IsNull matches rows where column IS NULL. This mirrors Prisma's `equals: null`
// scalar filter and cannot be expressed with [Equals] (SQL "= NULL" never
// matches).
func IsNull(column string) Condition { return nullCond{column: column} }

// IsNotNull matches rows where column IS NOT NULL, mirroring Prisma's
// `not: null` scalar filter.
func IsNotNull(column string) Condition { return nullCond{column: column, negate: true} }

// betweenCond compiles to "column [NOT] BETWEEN low AND high".
type betweenCond struct {
	column string
	low    any
	high   any
	negate bool
}

func (c betweenCond) compile(w *sqlWriter) string {
	op := "BETWEEN"
	if c.negate {
		op = "NOT BETWEEN"
	}
	return c.column + " " + op + " " + w.bind(c.low) + " AND " + w.bind(c.high)
}

// Between matches rows where column is within the inclusive range [low, high].
// It is shorthand for combining [Gte] and [Lte] and compiles to a single SQL
// BETWEEN term with both bounds bound as arguments.
func Between(column string, low, high any) Condition {
	return betweenCond{column: column, low: low, high: high}
}

// NotBetween matches rows where column falls outside the inclusive range
// [low, high], compiling to a SQL NOT BETWEEN term.
func NotBetween(column string, low, high any) Condition {
	return betweenCond{column: column, low: low, high: high, negate: true}
}

// notLikeCond compiles to "column NOT LIKE pattern".
type notLikeCond struct {
	column  string
	pattern string
}

func (c notLikeCond) compile(w *sqlWriter) string {
	return c.column + " NOT LIKE " + w.bind(c.pattern)
}

// NotContains matches rows where column does not contain the substring,
// mirroring Prisma's `not: { contains }`. Wildcard characters in substr are
// escaped so only literal matching is performed.
func NotContains(column, substr string) Condition {
	return notLikeCond{column, "%" + escapeLike(substr) + "%"}
}

// NotStartsWith matches rows where column does not begin with prefix, mirroring
// Prisma's `not: { startsWith }`.
func NotStartsWith(column, prefix string) Condition {
	return notLikeCond{column, escapeLike(prefix) + "%"}
}

// NotEndsWith matches rows where column does not end with suffix, mirroring
// Prisma's `not: { endsWith }`.
func NotEndsWith(column, suffix string) Condition {
	return notLikeCond{column, "%" + escapeLike(suffix)}
}

// iLikeCond compiles to "LOWER(column) LIKE pattern" with a pre-lowercased
// pattern, giving ASCII case-insensitive matching that is portable across
// engines (it does not rely on ILIKE or a collation).
type iLikeCond struct {
	column  string
	pattern string
}

func (c iLikeCond) compile(w *sqlWriter) string {
	return "LOWER(" + c.column + ") LIKE " + w.bind(c.pattern)
}

// IContains is the case-insensitive form of [Contains], mirroring Prisma's
// `contains` with `mode: "insensitive"`. Matching is performed by lowercasing
// both the column (via SQL LOWER) and the bound pattern (via Go
// strings.ToLower), so it is reliable for ASCII text on any engine.
func IContains(column, substr string) Condition {
	return iLikeCond{column, "%" + strings.ToLower(escapeLike(substr)) + "%"}
}

// IStartsWith is the case-insensitive form of [StartsWith] (Prisma's
// `startsWith` with `mode: "insensitive"`).
func IStartsWith(column, prefix string) Condition {
	return iLikeCond{column, strings.ToLower(escapeLike(prefix)) + "%"}
}

// IEndsWith is the case-insensitive form of [EndsWith] (Prisma's `endsWith`
// with `mode: "insensitive"`).
func IEndsWith(column, suffix string) Condition {
	return iLikeCond{column, "%" + strings.ToLower(escapeLike(suffix))}
}

// iEqualsCond compiles to "LOWER(column) = ?" with a pre-lowercased value.
type iEqualsCond struct {
	column string
	value  string
}

func (c iEqualsCond) compile(w *sqlWriter) string {
	return "LOWER(" + c.column + ") = " + w.bind(strings.ToLower(c.value))
}

// IEquals is the case-insensitive form of [Equals] for string columns,
// mirroring Prisma's `equals` with `mode: "insensitive"`. Both sides are
// lowercased (SQL LOWER and Go strings.ToLower) before comparison.
func IEquals(column, value string) Condition {
	return iEqualsCond{column: column, value: value}
}

// rawCond is a caller-supplied SQL fragment with "?" placeholders that are
// re-rendered in the active dialect's placeholder style and whose arguments are
// bound in order.
type rawCond struct {
	expr string
	args []any
}

func (c rawCond) compile(w *sqlWriter) string {
	parts := strings.Split(c.expr, "?")
	if len(parts)-1 != len(c.args) {
		w.fail(fmt.Errorf("prisma: RawCondition has %d placeholders but %d args", len(parts)-1, len(c.args)))
		return "1=0"
	}
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(p)
		if i < len(c.args) {
			b.WriteString(w.bind(c.args[i]))
		}
	}
	return b.String()
}

// RawCondition is an escape hatch for predicates the typed operators cannot
// express. The expr is an arbitrary boolean SQL fragment using "?" placeholders;
// each "?" is re-rendered in the client's [Dialect] placeholder style and the
// corresponding arg is bound safely (never concatenated). The number of "?"
// markers must equal len(args).
//
//	prisma.RawCondition("length(name) > ?", 3)
func RawCondition(expr string, args ...any) Condition {
	return rawCond{expr: expr, args: args}
}
