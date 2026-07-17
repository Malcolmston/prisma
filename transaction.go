package prisma

import (
	"context"
	"database/sql"
	"fmt"
)

// Tx is a handle to an in-progress database transaction. Start queries against
// it with [NewTxQuery]; every statement they run participates in the same
// transaction. A Tx is created by [Client.Transaction] (interactive) or
// [Client.Batch] (batched) and must not be used after the enclosing call
// returns.
type Tx struct {
	client *Client
	tx     *sql.Tx
}

// Client returns the owning [Client].
func (t *Tx) Client() *Client { return t.client }

// SQLTx returns the underlying *sql.Tx for escape-hatch use (e.g. raw
// statements not expressible through the builder).
func (t *Tx) SQLTx() *sql.Tx { return t.tx }

// Transaction runs fn inside a database transaction. If fn returns an error, or
// panics, the transaction is rolled back and the error (or a re-raised panic)
// is propagated; otherwise it is committed. This is the interactive transaction
// model: fn may issue any number of queries, inspect their results, and decide
// what to do next.
//
//	err := client.Transaction(ctx, func(tx *prisma.Tx) error {
//	    from, err := prisma.NewTxQuery[Account](tx).
//	        Where(prisma.Equals("id", 1)).FindUnique(ctx)
//	    if err != nil {
//	        return err
//	    }
//	    _, err = prisma.NewTxQuery[Account](tx).
//	        Where(prisma.Equals("id", 1)).
//	        Update(ctx, map[string]any{"balance": from.Balance - 100})
//	    return err
//	})
func (c *Client) Transaction(ctx context.Context, fn func(tx *Tx) error) (err error) {
	if c.db == nil {
		return fmt.Errorf("prisma: no *sql.DB configured")
	}
	sqlTx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return mapError(err)
	}
	tx := &Tx{client: c, tx: sqlTx}
	defer func() {
		if p := recover(); p != nil {
			_ = sqlTx.Rollback()
			panic(p)
		}
	}()
	if err = fn(tx); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err = sqlTx.Commit(); err != nil {
		return mapError(err)
	}
	return nil
}

// BatchFunc is a single operation in a [Client.Batch]. It receives the shared
// transaction and returns a per-operation result (typically a rows-affected
// count) or an error that aborts the whole batch.
type BatchFunc func(ctx context.Context, tx *Tx) (int64, error)

// Batch runs ops sequentially inside one transaction and returns each op's
// result. If any op returns an error the transaction is rolled back and no
// results are returned. This mirrors Prisma's array form of $transaction, where
// a list of independent writes commit or roll back together.
func (c *Client) Batch(ctx context.Context, ops ...BatchFunc) ([]int64, error) {
	results := make([]int64, 0, len(ops))
	err := c.Transaction(ctx, func(tx *Tx) error {
		for _, op := range ops {
			n, err := op(ctx, tx)
			if err != nil {
				return err
			}
			results = append(results, n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}
