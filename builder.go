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

// Nulls controls where NULL values sort relative to non-NULL values in an
// ORDER BY term.
type Nulls int

const (
	// NullsDefault leaves NULL ordering to the engine's default.
	NullsDefault Nulls = iota
	// NullsFirst sorts NULL values before non-NULL values.
	NullsFirst
	// NullsLast sorts NULL values after non-NULL values.
	NullsLast
)

type orderBy struct {
	column string
	dir    Direction
	nulls  Nulls
}

// cursor is a keyset pagination anchor: rows strictly after value in the
// column's sort direction are returned.
type cursor struct {
	column string
	value  any
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
	conn     execer
	model    *Model
	conds    []Condition
	orders   []orderBy
	sel      []string
	take     *int
	skip     *int
	includes []*Field
	distinct []string
	distAll  bool
	cur      *cursor
	skipDups bool
	err      error
}

// newWriter builds an sqlWriter seeded with the query's dialect, registry and
// base model so relation-filter and JSON conditions can resolve at compile time.
func (q *Query[T]) newWriter() *sqlWriter {
	return &sqlWriter{d: q.client.dialect, reg: q.client.registry, base: q.model}
}

// Where appends conditions, combined with the existing predicate using AND.
func (q *Query[T]) Where(conds ...Condition) *Query[T] {
	q.conds = append(q.conds, conds...)
	return q
}

// OrderBy adds an ORDER BY term. Multiple calls accumulate, producing a
// multi-column ordering in call order.
func (q *Query[T]) OrderBy(column string, dir Direction) *Query[T] {
	q.orders = append(q.orders, orderBy{column: column, dir: dir})
	return q
}

// OrderByNulls adds an ORDER BY term with explicit NULL placement. On engines
// without native NULLS syntax (see [Dialect.NativeNulls]) the placement is
// emulated with a leading "column IS NULL" sort term.
func (q *Query[T]) OrderByNulls(column string, dir Direction, nulls Nulls) *Query[T] {
	q.orders = append(q.orders, orderBy{column: column, dir: dir, nulls: nulls})
	return q
}

// Distinct restricts the result to distinct rows. With no columns it emits a
// plain SELECT DISTINCT; with columns on a dialect that supports it (see
// [Dialect.DistinctOn]) it emits SELECT DISTINCT ON (columns), otherwise it
// falls back to SELECT DISTINCT.
func (q *Query[T]) Distinct(columns ...string) *Query[T] {
	if len(columns) == 0 {
		q.distAll = true
	}
	q.distinct = append(q.distinct, columns...)
	return q
}

// Cursor sets a keyset pagination anchor: only rows strictly after value in the
// sort direction of column are returned. The column should also appear in an
// [Query.OrderBy] term; its direction determines whether the anchor uses "&gt;"
// (Asc) or "&lt;" (Desc). Combine with [Query.Skip] and [Query.Take] for
// forward pagination.
func (q *Query[T]) Cursor(column string, value any) *Query[T] {
	q.cur = &cursor{column: column, value: value}
	return q
}

// SkipDuplicates makes the next [Query.CreateMany] ignore rows that violate a
// unique constraint instead of failing (ON CONFLICT DO NOTHING / INSERT IGNORE,
// per the dialect's [Dialect.Upsert] style).
func (q *Query[T]) SkipDuplicates() *Query[T] {
	q.skipDups = true
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

// cursorCondition returns the keyset predicate implied by the cursor, or nil.
// The comparison direction follows the matching OrderBy term (Asc => ">",
// Desc => "<"); absent an explicit order it defaults to ascending.
func (q *Query[T]) cursorCondition() Condition {
	if q.cur == nil {
		return nil
	}
	dir := Asc
	for _, o := range q.orders {
		if o.column == q.cur.column {
			dir = o.dir
			break
		}
	}
	if dir == Desc {
		return Lt(q.cur.column, q.cur.value)
	}
	return Gt(q.cur.column, q.cur.value)
}

// whereClause compiles the accumulated conditions (plus any cursor predicate)
// into a " WHERE ..." fragment.
func (q *Query[T]) whereClause(w *sqlWriter) string {
	conds := q.conds
	if cc := q.cursorCondition(); cc != nil {
		conds = append(append([]Condition(nil), conds...), cc)
	}
	if len(conds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
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

// orderClause renders the ORDER BY fragment, qualifying base columns when the
// query joins and emulating NULLS placement on engines that lack native syntax.
func (q *Query[T]) orderClause(joined bool) string {
	if len(q.orders) == 0 {
		return ""
	}
	base := q.model.Table
	var terms []string
	for _, o := range q.orders {
		col := o.column
		if joined {
			if _, ok := q.model.FieldByColumn(col); ok {
				col = base + "." + col
			}
		}
		if o.nulls != NullsDefault && !q.client.dialect.NativeNulls {
			// Emulate: a boolean "col IS NULL" sorts NULLs (1) after non-NULLs
			// (0) ascending. NullsFirst wants them first, so sort that key DESC.
			nullDir := "ASC"
			if o.nulls == NullsFirst {
				nullDir = "DESC"
			}
			terms = append(terms, "("+col+" IS NULL) "+nullDir)
		}
		term := col + " " + string(o.dir)
		if o.nulls != NullsDefault && q.client.dialect.NativeNulls {
			if o.nulls == NullsFirst {
				term += " NULLS FIRST"
			} else {
				term += " NULLS LAST"
			}
		}
		terms = append(terms, term)
	}
	return " ORDER BY " + strings.Join(terms, ", ")
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
	b.WriteString(q.distinctPrefix(joined))
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(" FROM ")
	b.WriteString(base)
	for _, j := range joins {
		b.WriteString(" ")
		b.WriteString(j)
	}
	b.WriteString(q.whereClause(w))
	if w.err != nil {
		return "", nil, w.err
	}
	b.WriteString(q.orderClause(joined))
	if q.take != nil {
		b.WriteString(" LIMIT " + w.bind(*q.take))
	}
	if q.skip != nil {
		b.WriteString(" OFFSET " + w.bind(*q.skip))
	}
	return b.String(), plan, nil
}

// distinctPrefix renders the "DISTINCT" / "DISTINCT ON (...) " prefix that
// follows the SELECT keyword, or "" when the query is not distinct.
func (q *Query[T]) distinctPrefix(joined bool) string {
	if !q.distAll && len(q.distinct) == 0 {
		return ""
	}
	if len(q.distinct) > 0 && q.client.dialect.DistinctOn {
		cols := make([]string, len(q.distinct))
		for i, c := range q.distinct {
			if joined {
				if _, ok := q.model.FieldByColumn(c); ok {
					c = q.model.Table + "." + c
				}
			}
			cols[i] = c
		}
		return "DISTINCT ON (" + strings.Join(cols, ", ") + ") "
	}
	return "DISTINCT "
}

// FindManySQL returns the SELECT statement and bound arguments for FindMany.
func (q *Query[T]) FindManySQL() (string, []any, error) {
	w := q.newWriter()
	sqlStr, _, err := q.buildSelect(w)
	return sqlStr, w.args, err
}

// CountSQL returns the COUNT(*) statement and bound arguments.
func (q *Query[T]) CountSQL() (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	w := q.newWriter()
	var b strings.Builder
	b.WriteString("SELECT COUNT(*) FROM ")
	b.WriteString(q.model.Table)
	b.WriteString(q.whereClause(w))
	if w.err != nil {
		return "", nil, w.err
	}
	return b.String(), w.args, nil
}

// DeleteSQL returns the DELETE statement and bound arguments.
func (q *Query[T]) DeleteSQL() (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	w := q.newWriter()
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(q.model.Table)
	b.WriteString(q.whereClause(w))
	if w.err != nil {
		return "", nil, w.err
	}
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

	w := q.newWriter()
	var b strings.Builder
	b.WriteString("UPDATE ")
	b.WriteString(q.model.Table)
	b.WriteString(" SET ")
	sets := make([]string, len(cols))
	for i, c := range cols {
		if ue, ok := data[c].(updateExpr); ok {
			sets[i] = ue.compileUpdate(c, w)
		} else {
			sets[i] = c + " = " + w.bind(data[c])
		}
	}
	b.WriteString(strings.Join(sets, ", "))
	b.WriteString(q.whereClause(w))
	if w.err != nil {
		return "", nil, w.err
	}
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
	w := q.newWriter()
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
	w := q.newWriter()
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
	verb := "INSERT INTO"
	if q.skipDups && q.client.dialect.Upsert == OnDuplicateKey {
		verb = "INSERT IGNORE INTO"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s (%s) VALUES %s",
		verb, q.model.Table, strings.Join(cols, ", "), strings.Join(tuples, ", "))
	if q.skipDups && q.client.dialect.Upsert == OnConflict {
		b.WriteString(" ON CONFLICT DO NOTHING")
	}
	return b.String(), w.args, nil
}
