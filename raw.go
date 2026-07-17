package prisma

import (
	"context"
	"fmt"
	"reflect"
)

// QueryRaw runs an arbitrary SQL query with positional parameter binding and
// returns each row as a map from column name to value. Placeholders in the SQL
// must match the client's [Dialect]. This is Prisma's $queryRaw with dynamic
// (map) results.
func (c *Client) QueryRaw(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	if c.db == nil {
		return nil, errNoDB()
	}
	return queryRaw(ctx, c.db, query, args...)
}

// QueryRaw runs a raw query within the transaction; see [Client.QueryRaw].
func (t *Tx) QueryRaw(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	return queryRaw(ctx, t.tx, query, args...)
}

// ExecuteRaw runs an arbitrary non-query statement (INSERT/UPDATE/DELETE/DDL)
// with positional parameter binding and returns the number of rows affected.
// This is Prisma's $executeRaw.
func (c *Client) ExecuteRaw(ctx context.Context, query string, args ...any) (int64, error) {
	if c.db == nil {
		return 0, errNoDB()
	}
	return executeRaw(ctx, c.db, query, args...)
}

// ExecuteRaw runs a raw statement within the transaction; see
// [Client.ExecuteRaw].
func (t *Tx) ExecuteRaw(ctx context.Context, query string, args ...any) (int64, error) {
	return executeRaw(ctx, t.tx, query, args...)
}

// QueryRawInto runs a raw query and scans each row into a value of type T,
// matching result columns to T's mapped columns by name (unmatched columns are
// ignored, missing ones left zero). T must be registered on the client, or
// registrable. This is the typed form of [Client.QueryRaw].
func QueryRawInto[T any](ctx context.Context, c *Client, query string, args ...any) ([]T, error) {
	var zero T
	m, err := c.registry.Register(zero)
	if err != nil {
		return nil, err
	}
	if c.db == nil {
		return nil, errNoDB()
	}
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []T
	for rows.Next() {
		var rec T
		rv := reflect.ValueOf(&rec).Elem()
		dest := make([]any, len(cols))
		var scratch []any
		for i, col := range cols {
			if f, ok := m.FieldByColumn(col); ok {
				dest[i] = rv.FieldByIndex(f.Index).Addr().Interface()
			} else {
				scratch = append(scratch, new(any))
				dest[i] = scratch[len(scratch)-1]
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryRaw(ctx context.Context, conn execer, query string, args ...any) ([]map[string]any, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
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

func executeRaw(ctx context.Context, conn execer, query string, args ...any) (int64, error) {
	res, err := conn.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, mapError(err)
	}
	return res.RowsAffected()
}

func errNoDB() error { return fmt.Errorf("prisma: no *sql.DB configured") }
