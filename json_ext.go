package prisma

// JSONNotEquals matches rows where the value at path within the JSON column does
// not equal value. It is shorthand for [JSONPath] with the "<>" operator and
// mirrors Prisma's `not` JSON filter.
func JSONNotEquals(column string, path []string, value any) Condition {
	return jsonFilter{column: column, path: path, op: "<>", value: value}
}

// JSONGt matches rows where the extracted JSON value is greater than value,
// mirroring Prisma's `gt` JSON filter.
func JSONGt(column string, path []string, value any) Condition {
	return jsonFilter{column: column, path: path, op: ">", value: value}
}

// JSONGte matches rows where the extracted JSON value is greater than or equal
// to value, mirroring Prisma's `gte` JSON filter.
func JSONGte(column string, path []string, value any) Condition {
	return jsonFilter{column: column, path: path, op: ">=", value: value}
}

// JSONLt matches rows where the extracted JSON value is less than value,
// mirroring Prisma's `lt` JSON filter.
func JSONLt(column string, path []string, value any) Condition {
	return jsonFilter{column: column, path: path, op: "<", value: value}
}

// JSONLte matches rows where the extracted JSON value is less than or equal to
// value, mirroring Prisma's `lte` JSON filter.
func JSONLte(column string, path []string, value any) Condition {
	return jsonFilter{column: column, path: path, op: "<=", value: value}
}

// jsonLikeCond compiles to "<extract(column, path)> [NOT] LIKE ?".
type jsonLikeCond struct {
	column  string
	path    []string
	pattern string
	negate  bool
}

func (c jsonLikeCond) compile(w *sqlWriter) string {
	op := "LIKE"
	if c.negate {
		op = "NOT LIKE"
	}
	return w.d.extractJSON(c.column, c.path) + " " + op + " " + w.bind(c.pattern)
}

// JSONContains matches rows where the extracted JSON string value contains
// substr, mirroring Prisma's `string_contains` JSON filter. Wildcards in substr
// are escaped.
func JSONContains(column string, path []string, substr string) Condition {
	return jsonLikeCond{column: column, path: path, pattern: "%" + escapeLike(substr) + "%"}
}

// JSONNotContains matches rows where the extracted JSON string value does not
// contain substr, mirroring Prisma's `not: { string_contains }` JSON filter.
func JSONNotContains(column string, path []string, substr string) Condition {
	return jsonLikeCond{column: column, path: path, pattern: "%" + escapeLike(substr) + "%", negate: true}
}

// JSONStartsWith matches rows where the extracted JSON string value begins with
// prefix, mirroring Prisma's `string_starts_with` JSON filter.
func JSONStartsWith(column string, path []string, prefix string) Condition {
	return jsonLikeCond{column: column, path: path, pattern: escapeLike(prefix) + "%"}
}

// JSONEndsWith matches rows where the extracted JSON string value ends with
// suffix, mirroring Prisma's `string_ends_with` JSON filter.
func JSONEndsWith(column string, path []string, suffix string) Condition {
	return jsonLikeCond{column: column, path: path, pattern: "%" + escapeLike(suffix)}
}

// jsonNullCond compiles to "<extract(column, path)> IS [NOT] NULL".
type jsonNullCond struct {
	column string
	path   []string
	negate bool
}

func (c jsonNullCond) compile(w *sqlWriter) string {
	ex := w.d.extractJSON(c.column, c.path)
	if c.negate {
		return ex + " IS NOT NULL"
	}
	return ex + " IS NULL"
}

// JSONIsNull matches rows where the value at path within the JSON column is
// absent or JSON null (the extraction yields SQL NULL).
func JSONIsNull(column string, path []string) Condition {
	return jsonNullCond{column: column, path: path}
}

// JSONIsNotNull matches rows where the value at path within the JSON column is
// present and not null.
func JSONIsNotNull(column string, path []string) Condition {
	return jsonNullCond{column: column, path: path, negate: true}
}
