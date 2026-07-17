package prisma

import "strings"

// Condition is a boolean predicate used in WHERE clauses. Conditions compile
// to parameterized SQL fragments; literal values are always bound as arguments,
// never concatenated into the SQL text.
//
// Construct conditions with the operator helpers (Equals, Not, In, Lt, Gt,
// Contains, ...) and combine them with And, Or and NotGroup.
type Condition interface {
	compile(w *sqlWriter) string
}

type comparison struct {
	column string
	op     string
	value  any
}

func (c comparison) compile(w *sqlWriter) string {
	return c.column + " " + c.op + " " + w.bind(c.value)
}

// Equals matches rows where column = value.
func Equals(column string, value any) Condition {
	return comparison{column, "=", value}
}

// Not matches rows where column <> value.
func Not(column string, value any) Condition {
	return comparison{column, "<>", value}
}

// Lt matches rows where column < value.
func Lt(column string, value any) Condition { return comparison{column, "<", value} }

// Lte matches rows where column <= value.
func Lte(column string, value any) Condition { return comparison{column, "<=", value} }

// Gt matches rows where column > value.
func Gt(column string, value any) Condition { return comparison{column, ">", value} }

// Gte matches rows where column >= value.
func Gte(column string, value any) Condition { return comparison{column, ">=", value} }

type inCondition struct {
	column string
	values []any
	negate bool
}

func (c inCondition) compile(w *sqlWriter) string {
	if len(c.values) == 0 {
		// An empty IN list can never match; NOT IN always matches.
		if c.negate {
			return "1=1"
		}
		return "1=0"
	}
	marks := make([]string, len(c.values))
	for i, v := range c.values {
		marks[i] = w.bind(v)
	}
	op := "IN"
	if c.negate {
		op = "NOT IN"
	}
	return c.column + " " + op + " (" + strings.Join(marks, ", ") + ")"
}

// In matches rows where column is any of values.
func In(column string, values ...any) Condition {
	return inCondition{column: column, values: values}
}

// NotIn matches rows where column is none of values.
func NotIn(column string, values ...any) Condition {
	return inCondition{column: column, values: values, negate: true}
}

type likeCondition struct {
	column  string
	pattern string
}

func (c likeCondition) compile(w *sqlWriter) string {
	return c.column + " LIKE " + w.bind(c.pattern)
}

// Contains matches rows where column contains the substring (LIKE %s%).
func Contains(column, substr string) Condition {
	return likeCondition{column, "%" + escapeLike(substr) + "%"}
}

// StartsWith matches rows where column begins with the prefix (LIKE s%).
func StartsWith(column, prefix string) Condition {
	return likeCondition{column, escapeLike(prefix) + "%"}
}

// EndsWith matches rows where column ends with the suffix (LIKE %s).
func EndsWith(column, suffix string) Condition {
	return likeCondition{column, "%" + escapeLike(suffix)}
}

// escapeLike neutralizes LIKE wildcard characters in user input so that only
// the wildcards added by Contains/StartsWith/EndsWith are significant.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

type group struct {
	op    string
	conds []Condition
}

func (g group) compile(w *sqlWriter) string {
	if len(g.conds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(g.conds))
	for _, c := range g.conds {
		s := c.compile(w)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " "+g.op+" ") + ")"
}

// And combines conditions with logical AND.
func And(conds ...Condition) Condition { return group{"AND", conds} }

// Or combines conditions with logical OR.
func Or(conds ...Condition) Condition { return group{"OR", conds} }

type notGroup struct{ cond Condition }

func (n notGroup) compile(w *sqlWriter) string {
	s := n.cond.compile(w)
	if s == "" {
		return ""
	}
	return "NOT (" + s + ")"
}

// NotGroup negates a (possibly compound) condition: NOT ( ... ).
func NotGroup(cond Condition) Condition { return notGroup{cond} }
