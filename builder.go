package prisma

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Direction is a sort order for OrderBy.
type Direction string

const (
	// Asc sorts ascending.
	Asc Direction = "ASC"
	// Desc sorts descending.
	Desc Direction = "DESC"
)

type orderBy struct {
	column string
	dir    Direction
}

// scanTarget describes where a selected column's value should be scanned.
type scanTarget struct {
	field *Field // the column field
	rel   *Field // nil for a base-table column; otherwise the relation field
}

// Query is a fluent, type-safe builder for a single model T. Methods return the
// receiver so calls can be chained. Nothing touches the database until a
// terminal method (FindMany, Create, Count, ...) is called; the *SQL variants
// return the generated SQL and arguments without executing.
type Query[T any] struct {
	client   *Client
	model    *Model
	conds    []Condition
	orders   []orderBy
	sel      []string
	take     *int
	skip     *int
	includes []*Field
	err      error
}

// Where appends conditions, combined with the existing predicate using AND.
func (q *Query[T]) Where(conds ...Condition) *Query[T] {
	q.conds = append(q.conds, conds...)
	return q
}

// OrderBy adds an ORDER BY term.
func (q *Query[T]) OrderBy(column string, dir Direction) *Query[T] {
	q.orders = append(q.orders, orderBy{column, dir})
	return q
}

// Take limits the number of rows returned (SQL LIMIT).
func (q *Query[T]) Take(n int) *Query[T] {
	q.take = &n
	return q
}

// Skip offsets the returned rows (SQL OFFSET).
func (q *Query[T]) Skip(n int) *Query[T] {
	q.skip = &n
	return q
}

// Select restricts the projection to the named columns. Without it, all mapped
// columns are selected.
func (q *Query[T]) Select(columns ...string) *Query[T] {
	q.sel = append(q.sel, columns...)
	return q
}

// Include eagerly loads a to-one relation by its Go field name using a LEFT
// JOIN, selecting the related model's columns alongside the base row.
func (q *Query[T]) Include(relation string) *Query[T] {
	rel, ok := q.model.RelationByName(relation)
	if !ok {
		q.err = fmt.Errorf("prisma: %s has no relation %q", q.model.Name, relation)
		return q
	}
	q.includes = append(q.includes, rel)
	return q
}

// whereClause compiles the accumulated conditions into a " WHERE ..." fragment.
func (q *Query[T]) whereClause(w *sqlWriter) string {
	if len(q.conds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(q.conds))
	for _, c := range q.conds {
		s := c.compile(w)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

// relTargetModel resolves the compiled model for a relation field.
func (q *Query[T]) relTargetModel(rel *Field) (*Model, error) {
	m, ok := q.client.registry.ModelOf(rel.Relation.Target)
	if !ok {
		return nil, fmt.Errorf("prisma: relation target %s is not registered", rel.Relation.Target.Name())
	}
	return m, nil
}

// buildSelect assembles the SELECT statement and the scan plan describing how
// returned columns map back onto struct fields.
func (q *Query[T]) buildSelect(w *sqlWriter) (string, []scanTarget, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	base := q.model.Table
	joined := len(q.includes) > 0

	var cols []string
	var plan []scanTarget

	qualify := func(table, col string) string {
		if joined {
			return table + "." + col
		}
		return col
	}

	if len(q.sel) > 0 {
		for _, c := range q.sel {
			f, ok := q.model.FieldByColumn(c)
			if !ok {
				return "", nil, fmt.Errorf("prisma: %s has no column %q", q.model.Name, c)
			}
			cols = append(cols, qualify(base, f.Column))
			plan = append(plan, scanTarget{field: f})
		}
	} else {
		for _, f := range q.model.Fields {
			cols = append(cols, qualify(base, f.Column))
			plan = append(plan, scanTarget{field: f})
		}
	}

	var joins []string
	for _, rel := range q.includes {
		rm, err := q.relTargetModel(rel)
		if err != nil {
			return "", nil, err
		}
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s ON %s.%s = %s.%s",
			rm.Table, base, rel.Relation.ForeignKey, rm.Table, rel.Relation.References))
		for _, f := range rm.Fields {
			cols = append(cols, qualify(rm.Table, f.Column))
			plan = append(plan, scanTarget{field: f, rel: rel})
		}
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(" FROM ")
	b.WriteString(base)
	for _, j := range joins {
		b.WriteString(" ")
		b.WriteString(j)
	}
	b.WriteString(q.whereClause(w))

	if len(q.orders) > 0 {
		terms := make([]string, len(q.orders))
		for i, o := range q.orders {
			col := o.column
			if joined {
				if _, ok := q.model.FieldByColumn(col); ok {
					col = base + "." + col
				}
			}
			terms[i] = col + " " + string(o.dir)
		}
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(terms, ", "))
	}
	if q.take != nil {
		b.WriteString(" LIMIT " + w.bind(*q.take))
	}
	if q.skip != nil {
		b.WriteString(" OFFSET " + w.bind(*q.skip))
	}
	return b.String(), plan, nil
}

// FindManySQL returns the SELECT statement and bound arguments for FindMany.
func (q *Query[T]) FindManySQL() (string, []any, error) {
	w := &sqlWriter{d: q.client.dialect}
	sqlStr, _, err := q.buildSelect(w)
	return sqlStr, w.args, err
}

// CountSQL returns the COUNT(*) statement and bound arguments.
func (q *Query[T]) CountSQL() (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	w := &sqlWriter{d: q.client.dialect}
	var b strings.Builder
	b.WriteString("SELECT COUNT(*) FROM ")
	b.WriteString(q.model.Table)
	b.WriteString(q.whereClause(w))
	return b.String(), w.args, nil
}

// DeleteSQL returns the DELETE statement and bound arguments.
func (q *Query[T]) DeleteSQL() (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	w := &sqlWriter{d: q.client.dialect}
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(q.model.Table)
	b.WriteString(q.whereClause(w))
	return b.String(), w.args, nil
}

// UpdateSQL returns the UPDATE statement and bound arguments for the given
// column assignments. Column keys are sorted so the output is deterministic.
func (q *Query[T]) UpdateSQL(data map[string]any) (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	if len(data) == 0 {
		return "", nil, fmt.Errorf("prisma: Update requires at least one column")
	}
	cols := make([]string, 0, len(data))
	for c := range data {
		if _, ok := q.model.FieldByColumn(c); !ok {
			return "", nil, fmt.Errorf("prisma: %s has no column %q", q.model.Name, c)
		}
		cols = append(cols, c)
	}
	sort.Strings(cols)

	w := &sqlWriter{d: q.client.dialect}
	var b strings.Builder
	b.WriteString("UPDATE ")
	b.WriteString(q.model.Table)
	b.WriteString(" SET ")
	sets := make([]string, len(cols))
	for i, c := range cols {
		sets[i] = c + " = " + w.bind(data[c])
	}
	b.WriteString(strings.Join(sets, ", "))
	b.WriteString(q.whereClause(w))
	return b.String(), w.args, nil
}

// insertColumns returns the column fields written on INSERT (auto columns are
// skipped) together with their values read from value via reflection.
func (q *Query[T]) rowValues(value T) ([]*Field, []any) {
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	var fields []*Field
	var vals []any
	for _, f := range q.model.Fields {
		if f.IsAuto {
			continue
		}
		fields = append(fields, f)
		vals = append(vals, rv.FieldByIndex(f.Index).Interface())
	}
	return fields, vals
}

// CreateSQL returns the single-row INSERT statement and bound arguments.
func (q *Query[T]) CreateSQL(value T) (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	fields, vals := q.rowValues(value)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("prisma: nothing to insert for %s", q.model.Name)
	}
	w := &sqlWriter{d: q.client.dialect}
	cols := make([]string, len(fields))
	marks := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Column
		marks[i] = w.bind(vals[i])
	}
	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		q.model.Table, strings.Join(cols, ", "), strings.Join(marks, ", "))
	return sqlStr, w.args, nil
}

// CreateManySQL returns a multi-row INSERT statement and bound arguments.
func (q *Query[T]) CreateManySQL(values []T) (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	if len(values) == 0 {
		return "", nil, fmt.Errorf("prisma: CreateMany requires at least one row")
	}
	w := &sqlWriter{d: q.client.dialect}
	var cols []string
	tuples := make([]string, len(values))
	for i, v := range values {
		fields, vals := q.rowValues(v)
		if i == 0 {
			cols = make([]string, len(fields))
			for j, f := range fields {
				cols[j] = f.Column
			}
		}
		marks := make([]string, len(vals))
		for j := range vals {
			marks[j] = w.bind(vals[j])
		}
		tuples[i] = "(" + strings.Join(marks, ", ") + ")"
	}
	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		q.model.Table, strings.Join(cols, ", "), strings.Join(tuples, ", "))
	return sqlStr, w.args, nil
}
