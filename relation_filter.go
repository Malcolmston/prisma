package prisma

import (
	"fmt"
	"strings"
)

// relKind enumerates the relation quantifiers.
type relKind int

const (
	relSome  relKind = iota // at least one related row matches
	relEvery                // every related row matches
	relNone                 // no related row matches
	relIs                   // the single related row matches (to-one)
)

// relationFilter is a Condition that compiles to a correlated EXISTS/NOT EXISTS
// subquery over a named relation of the base model. It resolves the relation at
// compile time using the sqlWriter's registry and base model, so it can be
// freely nested inside And/Or/NotGroup and even inside other relation filters.
type relationFilter struct {
	relation string
	kind     relKind
	conds    []Condition
}

// Some matches base rows for which at least one related row (of the named
// to-many relation) satisfies conds. With no conditions it matches base rows
// that have any related row.
func Some(relation string, conds ...Condition) Condition {
	return relationFilter{relation: relation, kind: relSome, conds: conds}
}

// Every matches base rows for which every related row satisfies conds
// (vacuously true when there are no related rows), mirroring Prisma's "every".
func Every(relation string, conds ...Condition) Condition {
	return relationFilter{relation: relation, kind: relEvery, conds: conds}
}

// None matches base rows for which no related row satisfies conds (or that have
// no related rows at all), mirroring Prisma's "none".
func None(relation string, conds ...Condition) Condition {
	return relationFilter{relation: relation, kind: relNone, conds: conds}
}

// Is matches base rows whose single related row (of a to-one relation)
// satisfies conds, mirroring Prisma's "is".
func Is(relation string, conds ...Condition) Condition {
	return relationFilter{relation: relation, kind: relIs, conds: conds}
}

func (f relationFilter) compile(w *sqlWriter) string {
	if w.base == nil || w.reg == nil {
		w.fail(fmt.Errorf("prisma: relation filter %q requires a registered base model", f.relation))
		return "1=0"
	}
	rel, ok := w.base.RelationByName(f.relation)
	if !ok {
		w.fail(fmt.Errorf("prisma: %s has no relation %q", w.base.Name, f.relation))
		return "1=0"
	}
	target, ok := w.reg.ModelOf(rel.Relation.Target)
	if !ok {
		w.fail(fmt.Errorf("prisma: relation target %s is not registered", rel.Relation.Target.Name()))
		return "1=0"
	}

	// Correlation between the outer base row and the subquery's target rows.
	// For a to-many relation the foreign key lives on the target table and
	// references a base column; for a to-one relation it is the reverse.
	var correlation string
	if rel.Relation.Many {
		correlation = target.Table + "." + rel.Relation.ForeignKey + " = " +
			w.base.Table + "." + rel.Relation.References
	} else {
		correlation = w.base.Table + "." + rel.Relation.ForeignKey + " = " +
			target.Table + "." + rel.Relation.References
	}

	// Compile the inner predicate with the base model swapped to the target so
	// nested relation filters resolve against the correct model.
	inner := w.compileInner(target, f.conds)

	pred := correlation
	switch f.kind {
	case relEvery:
		// Every: no related row fails the predicate.
		if inner != "" {
			return "NOT EXISTS (SELECT 1 FROM " + target.Table + " WHERE " +
				correlation + " AND NOT (" + inner + "))"
		}
		return "NOT EXISTS (SELECT 1 FROM " + target.Table + " WHERE " + correlation + " AND 1=0)"
	case relNone:
		if inner != "" {
			pred = correlation + " AND " + inner
		}
		return "NOT EXISTS (SELECT 1 FROM " + target.Table + " WHERE " + pred + ")"
	default: // relSome, relIs
		if inner != "" {
			pred = correlation + " AND " + inner
		}
		return "EXISTS (SELECT 1 FROM " + target.Table + " WHERE " + pred + ")"
	}
}

// compileInner compiles a slice of conditions against target as the base model,
// AND-joined, restoring the previous base afterwards.
func (w *sqlWriter) compileInner(target *Model, conds []Condition) string {
	if len(conds) == 0 {
		return ""
	}
	prev := w.base
	w.base = target
	defer func() { w.base = prev }()
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		if s := c.compile(w); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, " AND ")
}

// jsonFilter is a Condition over a JSON column, comparing the value at a dotted
// path to a bound value using the dialect's JSON extraction syntax.
type jsonFilter struct {
	column string
	path   []string
	op     string
	value  any
}

// JSONPath matches rows where the value at path within the JSON column compares
// to value using op (e.g. "=", "<>", "<", ">"). The extraction syntax adapts to
// the dialect (json_extract for SQLite/MySQL, #>> for PostgreSQL).
//
//	prisma.JSONPath("data", []string{"address", "city"}, "=", "Paris")
func JSONPath(column string, path []string, op string, value any) Condition {
	return jsonFilter{column: column, path: path, op: op, value: value}
}

// JSONEquals matches rows where the value at path within the JSON column equals
// value. It is shorthand for [JSONPath] with the "=" operator.
func JSONEquals(column string, path []string, value any) Condition {
	return jsonFilter{column: column, path: path, op: "=", value: value}
}

func (f jsonFilter) compile(w *sqlWriter) string {
	return w.d.extractJSON(f.column, f.path) + " " + f.op + " " + w.bind(f.value)
}
