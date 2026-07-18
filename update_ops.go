package prisma

// updateExpr is implemented by update-data values that render their own SET
// assignment (referencing the target column) instead of a plain "col = ?" bind.
type updateExpr interface {
	compileUpdate(column string, w *sqlWriter) string
}

// UpdateOp is an atomic column update expression used as a value in the map
// passed to [Query.Update] / [Query.UpdateSQL]. Instead of assigning a literal,
// it computes the new value from the column's current value, mirroring Prisma's
// numeric atomic operations ({ increment }, { decrement }, { multiply },
// { divide }, { set }). The generated SQL is always parameterized, e.g.
// "balance = balance + ?".
type UpdateOp struct {
	op    string // "+", "-", "*", "/" or "" for a plain set
	value any
}

// compileUpdate renders the SET assignment for the given column.
func (o UpdateOp) compileUpdate(column string, w *sqlWriter) string {
	if o.op == "" {
		return column + " = " + w.bind(o.value)
	}
	return column + " = " + column + " " + o.op + " " + w.bind(o.value)
}

// Increment returns an [UpdateOp] that adds delta to the column's current value
// ("col = col + ?"), mirroring Prisma's `{ increment: delta }`.
func Increment(delta any) UpdateOp { return UpdateOp{op: "+", value: delta} }

// Decrement returns an [UpdateOp] that subtracts delta from the column's current
// value ("col = col - ?"), mirroring Prisma's `{ decrement: delta }`.
func Decrement(delta any) UpdateOp { return UpdateOp{op: "-", value: delta} }

// Multiply returns an [UpdateOp] that multiplies the column's current value by
// factor ("col = col * ?"), mirroring Prisma's `{ multiply: factor }`.
func Multiply(factor any) UpdateOp { return UpdateOp{op: "*", value: factor} }

// Divide returns an [UpdateOp] that divides the column's current value by
// divisor ("col = col / ?"), mirroring Prisma's `{ divide: divisor }`.
func Divide(divisor any) UpdateOp { return UpdateOp{op: "/", value: divisor} }

// SetTo returns an [UpdateOp] that assigns value directly ("col = ?"). It is the
// explicit form of Prisma's `{ set: value }` and is equivalent to placing value
// in the update map unwrapped; it exists so a set can sit alongside other
// [UpdateOp] values uniformly.
func SetTo(value any) UpdateOp { return UpdateOp{op: "", value: value} }
