package prisma

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Statement is a single generated SQL statement with its bound arguments, as
// produced by a nested write's planning method.
type Statement struct {
	// SQL is the statement text.
	SQL string
	// Args are the bound arguments, in placeholder order.
	Args []any
}

// nestedKind enumerates the nested-write operations on a relation.
type nestedKind int

const (
	nestCreate          nestedKind = iota // insert new related rows
	nestConnect                           // link existing rows by predicate
	nestConnectOrCreate                   // link if present, else insert
	nestUpdate                            // update related rows
)

// nestedChild is one nested operation against a named to-many relation.
type nestedChild struct {
	relation string
	kind     nestedKind
	rows     []any          // child structs for create / connectOrCreate
	where    []Condition    // predicate for connect / connectOrCreate / update
	data     map[string]any // assignments for update
}

// NestedCreate builds a parent INSERT together with dependent writes on its
// to-many relations (create / connect / connectOrCreate / nested update),
// executed as one transaction. The parent's generated primary key is propagated
// into each child statement's foreign key.
//
//	res, err := prisma.NewQuery[User](client).
//	    NestedCreate(User{Name: "Ada"}).
//	    Create("Posts", Post{Title: "Hello"}).
//	    Connect("Posts", prisma.Equals("id", 7)).
//	    Exec(ctx)
type NestedCreate[T any] struct {
	q     *Query[T]
	value T
	ops   []nestedChild
	err   error
}

// NestedCreate starts a nested write whose root inserts value.
func (q *Query[T]) NestedCreate(value T) *NestedCreate[T] {
	return &NestedCreate[T]{q: q, value: value, err: q.err}
}

// Create schedules inserting each child into the named relation, with its
// foreign key set to the new parent's primary key.
func (n *NestedCreate[T]) Create(relation string, children ...any) *NestedCreate[T] {
	n.ops = append(n.ops, nestedChild{relation: relation, kind: nestCreate, rows: children})
	return n
}

// Connect schedules linking existing related rows matching where to the new
// parent (an UPDATE of their foreign key).
func (n *NestedCreate[T]) Connect(relation string, where ...Condition) *NestedCreate[T] {
	n.ops = append(n.ops, nestedChild{relation: relation, kind: nestConnect, where: where})
	return n
}

// ConnectOrCreate links the related row matching where if it exists, otherwise
// inserts child. The choice is made at execution time via a existence check.
func (n *NestedCreate[T]) ConnectOrCreate(relation string, where []Condition, child any) *NestedCreate[T] {
	n.ops = append(n.ops, nestedChild{relation: relation, kind: nestConnectOrCreate, where: where, rows: []any{child}})
	return n
}

// Update schedules updating related rows matching where (scoped to the new
// parent) with data.
func (n *NestedCreate[T]) Update(relation string, where []Condition, data map[string]any) *NestedCreate[T] {
	n.ops = append(n.ops, nestedChild{relation: relation, kind: nestUpdate, where: where, data: data})
	return n
}

// resolveRelation returns the relation field and its compiled target model, or
// an error if the relation is unknown, not to-many, or unregistered.
func (n *NestedCreate[T]) resolveRelation(name string) (*Field, *Model, error) {
	rel, ok := n.q.model.RelationByName(name)
	if !ok {
		return nil, nil, fmt.Errorf("prisma: %s has no relation %q", n.q.model.Name, name)
	}
	if !rel.Relation.Many {
		return nil, nil, fmt.Errorf("prisma: nested write on %q requires a to-many relation", name)
	}
	tm, ok := n.q.client.registry.ModelOf(rel.Relation.Target)
	if !ok {
		return nil, nil, fmt.Errorf("prisma: relation target %s is not registered", rel.Relation.Target.Name())
	}
	return rel, tm, nil
}

// Statements returns the parent INSERT followed by the deterministic child
// statements, using parentID as the linking foreign-key value. A
// connectOrCreate operation is rendered as its create (INSERT) branch; at
// execution time [NestedCreate.Exec] chooses connect or create per row.
func (n *NestedCreate[T]) Statements(parentID any) ([]Statement, error) {
	if n.err != nil {
		return nil, n.err
	}
	parentSQL, parentArgs, err := n.q.CreateSQL(n.value)
	if err != nil {
		return nil, err
	}
	stmts := []Statement{{SQL: parentSQL, Args: parentArgs}}
	for _, op := range n.ops {
		rel, tm, err := n.resolveRelation(op.relation)
		if err != nil {
			return nil, err
		}
		fk := rel.Relation.ForeignKey
		switch op.kind {
		case nestCreate, nestConnectOrCreate:
			for _, child := range op.rows {
				st, err := childInsertStatement(n.q.client.dialect, tm, child, fk, parentID)
				if err != nil {
					return nil, err
				}
				stmts = append(stmts, st)
			}
		case nestConnect:
			st, err := connectStatement(n.q.client, tm, fk, parentID, op.where)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, st)
		case nestUpdate:
			st, err := childUpdateStatement(n.q.client, tm, fk, parentID, op.where, op.data)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, st)
		}
	}
	return stmts, nil
}

// Exec runs the nested write in a transaction: the parent is inserted first,
// then each dependent operation runs with the parent's generated primary key.
// The returned value is the parent with its auto primary key populated.
func (n *NestedCreate[T]) Exec(ctx context.Context) (T, error) {
	if n.err != nil {
		return n.value, n.err
	}
	result := n.value
	err := n.q.client.Transaction(ctx, func(tx *Tx) error {
		parent, err := NewTxQuery[T](tx).Create(ctx, n.value)
		if err != nil {
			return err
		}
		result = parent
		pid, err := parentPK(n.q.model, parent)
		if err != nil {
			return err
		}
		for _, op := range n.ops {
			if err := n.execChild(ctx, tx, op, pid); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

// execChild runs one nested operation within tx using the resolved parent id.
func (n *NestedCreate[T]) execChild(ctx context.Context, tx *Tx, op nestedChild, pid any) error {
	rel, tm, err := n.resolveRelation(op.relation)
	if err != nil {
		return err
	}
	fk := rel.Relation.ForeignKey
	switch op.kind {
	case nestCreate:
		for _, child := range op.rows {
			st, err := childInsertStatement(n.q.client.dialect, tm, child, fk, pid)
			if err != nil {
				return err
			}
			if _, err := executeRaw(ctx, tx.tx, st.SQL, st.Args...); err != nil {
				return err
			}
		}
	case nestConnect:
		st, err := connectStatement(n.q.client, tm, fk, pid, op.where)
		if err != nil {
			return err
		}
		if _, err := executeRaw(ctx, tx.tx, st.SQL, st.Args...); err != nil {
			return err
		}
	case nestUpdate:
		st, err := childUpdateStatement(n.q.client, tm, fk, pid, op.where, op.data)
		if err != nil {
			return err
		}
		if _, err := executeRaw(ctx, tx.tx, st.SQL, st.Args...); err != nil {
			return err
		}
	case nestConnectOrCreate:
		exists, err := childExists(ctx, tx, n.q.client, tm, op.where)
		if err != nil {
			return err
		}
		if exists {
			st, err := connectStatement(n.q.client, tm, fk, pid, op.where)
			if err != nil {
				return err
			}
			if _, err := executeRaw(ctx, tx.tx, st.SQL, st.Args...); err != nil {
				return err
			}
		} else {
			st, err := childInsertStatement(n.q.client.dialect, tm, op.rows[0], fk, pid)
			if err != nil {
				return err
			}
			if _, err := executeRaw(ctx, tx.tx, st.SQL, st.Args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// childInsertStatement builds an INSERT for a child struct with its foreign-key
// column forced to parentID.
func childInsertStatement(d Dialect, tm *Model, child any, fk string, parentID any) (Statement, error) {
	rv := reflect.ValueOf(child)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct || rv.Type() != tm.Type {
		return Statement{}, fmt.Errorf("prisma: nested child must be a %s, got %s", tm.Name, rv.Type())
	}
	w := &sqlWriter{d: d}
	var cols, marks []string
	haveFK := false
	for _, f := range tm.Fields {
		if f.IsAuto {
			continue
		}
		val := rv.FieldByIndex(f.Index).Interface()
		if f.Column == fk {
			val = parentID
			haveFK = true
		}
		cols = append(cols, f.Column)
		marks = append(marks, w.bind(val))
	}
	if !haveFK {
		cols = append(cols, fk)
		marks = append(marks, w.bind(parentID))
	}
	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tm.Table, strings.Join(cols, ", "), strings.Join(marks, ", "))
	return Statement{SQL: sqlStr, Args: w.args}, nil
}

// connectStatement builds an UPDATE that points matching rows' foreign key at
// parentID.
func connectStatement(c *Client, tm *Model, fk string, parentID any, where []Condition) (Statement, error) {
	w := &sqlWriter{d: c.dialect, reg: c.registry, base: tm}
	set := fk + " = " + w.bind(parentID)
	whereSQL := compileConds(w, where)
	if w.err != nil {
		return Statement{}, w.err
	}
	sqlStr := "UPDATE " + tm.Table + " SET " + set + whereSQL
	return Statement{SQL: sqlStr, Args: w.args}, nil
}

// childUpdateStatement builds an UPDATE scoped to the parent (fk = parentID)
// applying data to rows also matching where.
func childUpdateStatement(c *Client, tm *Model, fk string, parentID any, where []Condition, data map[string]any) (Statement, error) {
	if len(data) == 0 {
		return Statement{}, fmt.Errorf("prisma: nested update requires at least one column")
	}
	cols := sortedKeys(data)
	w := &sqlWriter{d: c.dialect, reg: c.registry, base: tm}
	sets := make([]string, len(cols))
	for i, col := range cols {
		if _, ok := tm.FieldByColumn(col); !ok {
			return Statement{}, fmt.Errorf("prisma: %s has no column %q", tm.Name, col)
		}
		sets[i] = col + " = " + w.bind(data[col])
	}
	// Scope to the parent, plus any caller predicate.
	conds := append([]Condition{Equals(fk, parentID)}, where...)
	whereSQL := compileConds(w, conds)
	if w.err != nil {
		return Statement{}, w.err
	}
	sqlStr := "UPDATE " + tm.Table + " SET " + strings.Join(sets, ", ") + whereSQL
	return Statement{SQL: sqlStr, Args: w.args}, nil
}

// childExists reports whether any child row matches where.
func childExists(ctx context.Context, tx *Tx, c *Client, tm *Model, where []Condition) (bool, error) {
	w := &sqlWriter{d: c.dialect, reg: c.registry, base: tm}
	whereSQL := compileConds(w, where)
	if w.err != nil {
		return false, w.err
	}
	sqlStr := "SELECT COUNT(*) FROM " + tm.Table + whereSQL
	var n int64
	if err := tx.tx.QueryRowContext(ctx, sqlStr, w.args...).Scan(&n); err != nil {
		return false, mapError(err)
	}
	return n > 0, nil
}

// compileConds AND-joins conditions into a " WHERE ..." fragment (or "").
func compileConds(w *sqlWriter, conds []Condition) string {
	if len(conds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		if s := c.compile(w); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

// parentPK extracts the primary-key value from a parent struct.
func parentPK[T any](m *Model, parent T) (any, error) {
	if m.PK == nil {
		return nil, fmt.Errorf("prisma: %s has no primary key for nested write", m.Name)
	}
	rv := reflect.ValueOf(parent)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	return rv.FieldByIndex(m.PK.Index).Interface(), nil
}

// sortedKeys returns the map keys sorted for deterministic SQL.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
