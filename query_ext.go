package prisma

import (
	"context"
	"fmt"
	"strings"
)

// FindFirstOrThrow executes the query like [Query.FindFirst] but returns the
// typed [ErrRecordNotFound] (Prisma code P2025) instead of a plain sentinel when
// no row matches, mirroring Prisma's findFirstOrThrow. Any other error is
// returned unchanged.
func (q *Query[T]) FindFirstOrThrow(ctx context.Context) (T, error) {
	v, err := q.FindFirst(ctx)
	if err == ErrNotFound {
		var zero T
		return zero, ErrRecordNotFound
	}
	return v, err
}

// FindUniqueOrThrow executes the query like [Query.FindUnique] but returns the
// typed [ErrRecordNotFound] (Prisma code P2025) when no row matches, mirroring
// Prisma's findUniqueOrThrow.
func (q *Query[T]) FindUniqueOrThrow(ctx context.Context) (T, error) {
	return q.FindFirstOrThrow(ctx)
}

// ExistsSQL returns a statement that evaluates to a single boolean-ish row
// reporting whether any row matches the query's WHERE predicate, of the form
// "SELECT EXISTS(SELECT 1 FROM table WHERE ...)". It does not touch the
// database.
func (q *Query[T]) ExistsSQL() (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	w := q.newWriter()
	var b strings.Builder
	b.WriteString("SELECT EXISTS(SELECT 1 FROM ")
	b.WriteString(q.model.Table)
	b.WriteString(q.whereClause(w))
	b.WriteString(")")
	if w.err != nil {
		return "", nil, w.err
	}
	return b.String(), w.args, nil
}

// Exists reports whether at least one row matches the query's WHERE predicate,
// evaluating the [Query.ExistsSQL] statement. It is a convenience over counting
// when only presence matters.
func (q *Query[T]) Exists(ctx context.Context) (bool, error) {
	sqlStr, args, err := q.ExistsSQL()
	if err != nil {
		return false, err
	}
	conn, err := q.executor()
	if err != nil {
		return false, err
	}
	var b bool
	if err := conn.QueryRowContext(ctx, sqlStr, args...).Scan(&b); err != nil {
		return false, mapError(err)
	}
	return b, nil
}

// CountDistinctSQL returns a "SELECT COUNT(DISTINCT column) FROM table WHERE ..."
// statement and its bound arguments without executing it. The column must be a
// mapped column of the model.
func (q *Query[T]) CountDistinctSQL(column string) (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	if _, ok := q.model.FieldByColumn(column); !ok {
		return "", nil, fmt.Errorf("prisma: %s has no column %q", q.model.Name, column)
	}
	w := q.newWriter()
	var b strings.Builder
	b.WriteString("SELECT COUNT(DISTINCT ")
	b.WriteString(column)
	b.WriteString(") FROM ")
	b.WriteString(q.model.Table)
	b.WriteString(q.whereClause(w))
	if w.err != nil {
		return "", nil, w.err
	}
	return b.String(), w.args, nil
}

// CountDistinct executes [Query.CountDistinctSQL] and returns the number of
// distinct non-NULL values of column among the matching rows, mirroring
// Prisma's `_count` on a distinct field.
func (q *Query[T]) CountDistinct(ctx context.Context, column string) (int64, error) {
	sqlStr, args, err := q.CountDistinctSQL(column)
	if err != nil {
		return 0, err
	}
	conn, err := q.executor()
	if err != nil {
		return 0, err
	}
	var n int64
	if err := conn.QueryRowContext(ctx, sqlStr, args...).Scan(&n); err != nil {
		return 0, mapError(err)
	}
	return n, nil
}

// Paginate sets LIMIT/OFFSET for 1-based offset pagination: page 1 returns the
// first size rows, page 2 the next size, and so on. It is a convenience over
// setting [Query.Take] and [Query.Skip] by hand. page and size are clamped to a
// minimum of 1 and 0 respectively.
func (q *Query[T]) Paginate(page, size int) *Query[T] {
	if page < 1 {
		page = 1
	}
	if size < 0 {
		size = 0
	}
	skip := (page - 1) * size
	q.take = &size
	q.skip = &skip
	return q
}

// Clone returns an independent copy of the query whose slice-valued state
// (conditions, ordering, selection, includes, distinct columns) can be extended
// without affecting the original. It lets a partially-built query serve as a
// reusable base for several derived queries.
func (q *Query[T]) Clone() *Query[T] {
	cp := *q
	cp.conds = append([]Condition(nil), q.conds...)
	cp.orders = append([]orderBy(nil), q.orders...)
	cp.sel = append([]string(nil), q.sel...)
	cp.includes = append([]*Field(nil), q.includes...)
	cp.distinct = append([]string(nil), q.distinct...)
	if q.take != nil {
		t := *q.take
		cp.take = &t
	}
	if q.skip != nil {
		s := *q.skip
		cp.skip = &s
	}
	if q.cur != nil {
		c := *q.cur
		cp.cur = &c
	}
	return &cp
}
