package prisma

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// aggFunc is a SQL aggregate function name.
type aggFunc string

const (
	aggCount aggFunc = "COUNT"
	aggSum   aggFunc = "SUM"
	aggAvg   aggFunc = "AVG"
	aggMin   aggFunc = "MIN"
	aggMax   aggFunc = "MAX"
)

// aggTerm is one aggregate projection: fn(column) AS alias. A count of all rows
// uses column "*".
type aggTerm struct {
	fn     aggFunc
	column string
	alias  string
}

func (t aggTerm) sql() string {
	return string(t.fn) + "(" + t.column + ") AS " + t.alias
}

// AggregateResult holds the scalar results of an [AggregateQuery]. Count is the
// row count (present only when Count was requested). The maps are keyed by
// column name and present only for the functions that were requested.
type AggregateResult struct {
	// Count is the number of matching rows (COUNT(*)).
	Count int64
	// Sum holds SUM(column) per requested column.
	Sum map[string]float64
	// Avg holds AVG(column) per requested column.
	Avg map[string]float64
	// Min holds MIN(column) per requested column.
	Min map[string]float64
	// Max holds MAX(column) per requested column.
	Max map[string]float64
}

// AggregateQuery builds an aggregation over the rows selected by a base
// [Query]. Chain [AggregateQuery.Count], [AggregateQuery.Sum],
// [AggregateQuery.Avg], [AggregateQuery.Min] and [AggregateQuery.Max], then call
// [AggregateQuery.Exec] (or [AggregateQuery.SQL] to inspect the statement).
type AggregateQuery[T any] struct {
	q     *Query[T]
	terms []aggTerm
	err   error
}

// Aggregate begins an aggregation over the query's WHERE predicate.
func (q *Query[T]) Aggregate() *AggregateQuery[T] {
	return &AggregateQuery[T]{q: q, err: q.err}
}

// Count adds COUNT(*) to the aggregation.
func (a *AggregateQuery[T]) Count() *AggregateQuery[T] {
	a.terms = append(a.terms, aggTerm{fn: aggCount, column: "*", alias: "_count"})
	return a
}

// Sum adds SUM(column) for each column.
func (a *AggregateQuery[T]) Sum(columns ...string) *AggregateQuery[T] {
	return a.add(aggSum, "_sum_", columns)
}

// Avg adds AVG(column) for each column.
func (a *AggregateQuery[T]) Avg(columns ...string) *AggregateQuery[T] {
	return a.add(aggAvg, "_avg_", columns)
}

// Min adds MIN(column) for each column.
func (a *AggregateQuery[T]) Min(columns ...string) *AggregateQuery[T] {
	return a.add(aggMin, "_min_", columns)
}

// Max adds MAX(column) for each column.
func (a *AggregateQuery[T]) Max(columns ...string) *AggregateQuery[T] {
	return a.add(aggMax, "_max_", columns)
}

func (a *AggregateQuery[T]) add(fn aggFunc, prefix string, columns []string) *AggregateQuery[T] {
	for _, c := range columns {
		if _, ok := a.q.model.FieldByColumn(c); !ok && a.err == nil {
			a.err = fmt.Errorf("prisma: %s has no column %q", a.q.model.Name, c)
		}
		a.terms = append(a.terms, aggTerm{fn: fn, column: c, alias: prefix + c})
	}
	return a
}

// SQL returns the aggregate SELECT statement and bound arguments without
// executing it.
func (a *AggregateQuery[T]) SQL() (string, []any, error) {
	if a.err != nil {
		return "", nil, a.err
	}
	if len(a.terms) == 0 {
		return "", nil, fmt.Errorf("prisma: Aggregate requires at least one function")
	}
	w := a.q.newWriter()
	sels := make([]string, len(a.terms))
	for i, t := range a.terms {
		sels[i] = t.sql()
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(sels, ", "))
	b.WriteString(" FROM ")
	b.WriteString(a.q.model.Table)
	b.WriteString(a.q.whereClause(w))
	if w.err != nil {
		return "", nil, w.err
	}
	return b.String(), w.args, nil
}

// Exec runs the aggregation and returns the scalar results.
func (a *AggregateQuery[T]) Exec(ctx context.Context) (AggregateResult, error) {
	var res AggregateResult
	sqlStr, args, err := a.SQL()
	if err != nil {
		return res, err
	}
	conn, err := a.q.executor()
	if err != nil {
		return res, err
	}
	dest := make([]any, len(a.terms))
	holders := make([]sql.NullFloat64, len(a.terms))
	var cnt int64
	for i, t := range a.terms {
		if t.fn == aggCount {
			dest[i] = &cnt
		} else {
			dest[i] = &holders[i]
		}
	}
	if err := conn.QueryRowContext(ctx, sqlStr, args...).Scan(dest...); err != nil {
		return res, mapError(err)
	}
	res.Count = cnt
	for i, t := range a.terms {
		if t.fn == aggCount || !holders[i].Valid {
			continue
		}
		switch t.fn {
		case aggSum:
			putFloat(&res.Sum, t.column, holders[i].Float64)
		case aggAvg:
			putFloat(&res.Avg, t.column, holders[i].Float64)
		case aggMin:
			putFloat(&res.Min, t.column, holders[i].Float64)
		case aggMax:
			putFloat(&res.Max, t.column, holders[i].Float64)
		}
	}
	return res, nil
}

func putFloat(m *map[string]float64, k string, v float64) {
	if *m == nil {
		*m = map[string]float64{}
	}
	(*m)[k] = v
}

// GroupQuery builds a GROUP BY aggregation over the rows selected by a base
// [Query]. It selects the grouping columns plus any requested aggregates, and
// supports a HAVING filter over the aggregated rows.
type GroupQuery[T any] struct {
	q      *Query[T]
	groups []string
	terms  []aggTerm
	having []Condition
	err    error
}

// GroupBy begins a grouped aggregation keyed by the given columns.
func (q *Query[T]) GroupBy(columns ...string) *GroupQuery[T] {
	g := &GroupQuery[T]{q: q, groups: columns, err: q.err}
	for _, c := range columns {
		if _, ok := q.model.FieldByColumn(c); !ok && g.err == nil {
			g.err = fmt.Errorf("prisma: %s has no column %q", q.model.Name, c)
		}
	}
	return g
}

// Count adds COUNT(*) to each group.
func (g *GroupQuery[T]) Count() *GroupQuery[T] {
	g.terms = append(g.terms, aggTerm{fn: aggCount, column: "*", alias: "_count"})
	return g
}

// Sum adds SUM(column) to each group for every column.
func (g *GroupQuery[T]) Sum(columns ...string) *GroupQuery[T] {
	return g.add(aggSum, "_sum_", columns)
}

// Avg adds AVG(column) to each group for every column.
func (g *GroupQuery[T]) Avg(columns ...string) *GroupQuery[T] {
	return g.add(aggAvg, "_avg_", columns)
}

// Min adds MIN(column) to each group for every column.
func (g *GroupQuery[T]) Min(columns ...string) *GroupQuery[T] {
	return g.add(aggMin, "_min_", columns)
}

// Max adds MAX(column) to each group for every column.
func (g *GroupQuery[T]) Max(columns ...string) *GroupQuery[T] {
	return g.add(aggMax, "_max_", columns)
}

// Having adds a filter applied after grouping. Conditions typically reference
// an aggregate alias (e.g. prisma.Gt("_count", 1)) or a raw aggregate
// expression.
func (g *GroupQuery[T]) Having(conds ...Condition) *GroupQuery[T] {
	g.having = append(g.having, conds...)
	return g
}

func (g *GroupQuery[T]) add(fn aggFunc, prefix string, columns []string) *GroupQuery[T] {
	for _, c := range columns {
		if _, ok := g.q.model.FieldByColumn(c); !ok && g.err == nil {
			g.err = fmt.Errorf("prisma: %s has no column %q", g.q.model.Name, c)
		}
		g.terms = append(g.terms, aggTerm{fn: fn, column: c, alias: prefix + c})
	}
	return g
}

// SQL returns the GROUP BY SELECT statement and bound arguments without
// executing it.
func (g *GroupQuery[T]) SQL() (string, []any, error) {
	if g.err != nil {
		return "", nil, g.err
	}
	if len(g.groups) == 0 {
		return "", nil, fmt.Errorf("prisma: GroupBy requires at least one column")
	}
	w := g.q.newWriter()
	sels := append([]string(nil), g.groups...)
	for _, t := range g.terms {
		sels = append(sels, t.sql())
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(sels, ", "))
	b.WriteString(" FROM ")
	b.WriteString(g.q.model.Table)
	b.WriteString(g.q.whereClause(w))
	b.WriteString(" GROUP BY ")
	b.WriteString(strings.Join(g.groups, ", "))
	if len(g.having) > 0 {
		parts := make([]string, 0, len(g.having))
		for _, c := range g.having {
			if s := c.compile(w); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			b.WriteString(" HAVING ")
			b.WriteString(strings.Join(parts, " AND "))
		}
	}
	b.WriteString(g.q.orderClause(false))
	if w.err != nil {
		return "", nil, w.err
	}
	return b.String(), w.args, nil
}

// Exec runs the grouped aggregation and returns one row per group. Each row maps
// column/alias names to their scanned values (grouping keys and aggregates).
func (g *GroupQuery[T]) Exec(ctx context.Context) ([]map[string]any, error) {
	sqlStr, args, err := g.SQL()
	if err != nil {
		return nil, err
	}
	conn, err := g.q.executor()
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = cells[i]
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
