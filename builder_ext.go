package prisma

// WhereRaw appends a raw SQL boolean fragment to the query's predicate,
// combined with any existing conditions using AND. The expr uses "?"
// placeholders that are re-rendered in the client's [Dialect] and whose args are
// bound safely; it is the [Query]-level shorthand for
// Where([RawCondition](expr, args...)).
func (q *Query[T]) WhereRaw(expr string, args ...any) *Query[T] {
	q.conds = append(q.conds, RawCondition(expr, args...))
	return q
}

// OrWhere adds a single predicate that is the logical OR of conds, combined with
// the rest of the query's predicate using AND. It is shorthand for
// Where([Or](conds...)) and mirrors Prisma's top-level `OR` array.
//
//	q.Where(prisma.Equals("active", true)).
//	  OrWhere(prisma.Equals("role", "admin"), prisma.Equals("role", "owner"))
//	// ... WHERE active = ? AND (role = ? OR role = ?)
func (q *Query[T]) OrWhere(conds ...Condition) *Query[T] {
	q.conds = append(q.conds, Or(conds...))
	return q
}
