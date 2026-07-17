package prisma

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// UpsertSQL returns an "insert-or-update" statement and its bound arguments. If
// a row conflicting on conflictColumns already exists, the update assignments
// are applied; otherwise create is inserted. The syntax follows the dialect's
// [Dialect.Upsert] style: ON CONFLICT (PostgreSQL/SQLite) or ON DUPLICATE KEY
// UPDATE (MySQL). Update column keys are sorted for deterministic output.
func (q *Query[T]) UpsertSQL(conflictColumns []string, create T, update map[string]any) (string, []any, error) {
	if q.err != nil {
		return "", nil, q.err
	}
	if len(update) == 0 {
		return "", nil, fmt.Errorf("prisma: Upsert requires at least one update column")
	}
	fields, vals := q.rowValues(create)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("prisma: nothing to insert for %s", q.model.Name)
	}
	updCols := make([]string, 0, len(update))
	for c := range update {
		if _, ok := q.model.FieldByColumn(c); !ok {
			return "", nil, fmt.Errorf("prisma: %s has no column %q", q.model.Name, c)
		}
		updCols = append(updCols, c)
	}
	sort.Strings(updCols)

	w := q.newWriter()
	cols := make([]string, len(fields))
	marks := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Column
		marks[i] = w.bind(vals[i])
	}

	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES (%s)",
		q.model.Table, strings.Join(cols, ", "), strings.Join(marks, ", "))

	sets := make([]string, len(updCols))
	if q.client.dialect.Upsert == OnDuplicateKey {
		for i, c := range updCols {
			sets[i] = c + " = " + w.bind(update[c])
		}
		b.WriteString(" ON DUPLICATE KEY UPDATE ")
		b.WriteString(strings.Join(sets, ", "))
	} else {
		if len(conflictColumns) == 0 {
			return "", nil, fmt.Errorf("prisma: Upsert requires conflict columns for ON CONFLICT")
		}
		for i, c := range updCols {
			sets[i] = c + " = " + w.bind(update[c])
		}
		fmt.Fprintf(&b, " ON CONFLICT (%s) DO UPDATE SET %s",
			strings.Join(conflictColumns, ", "), strings.Join(sets, ", "))
	}
	return b.String(), w.args, nil
}

// Upsert executes an insert-or-update (see [Query.UpsertSQL]) and returns the
// number of rows affected.
func (q *Query[T]) Upsert(ctx context.Context, conflictColumns []string, create T, update map[string]any) (int64, error) {
	sqlStr, args, err := q.UpsertSQL(conflictColumns, create, update)
	if err != nil {
		return 0, err
	}
	return q.exec(ctx, sqlStr, args)
}
