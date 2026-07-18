package prisma

import (
	"reflect"
	"testing"
)

func TestJSONExtSQLite(t *testing.T) {
	path := []string{"address", "city"}
	cases := []struct {
		name string
		cond Condition
		sql  string
		args []any
	}{
		{"JSONNotEquals", JSONNotEquals("data", path, "Paris"), "json_extract(data, '$.address.city') <> ?", []any{"Paris"}},
		{"JSONGt", JSONGt("data", []string{"age"}, 18), "json_extract(data, '$.age') > ?", []any{18}},
		{"JSONGte", JSONGte("data", []string{"age"}, 18), "json_extract(data, '$.age') >= ?", []any{18}},
		{"JSONLt", JSONLt("data", []string{"age"}, 65), "json_extract(data, '$.age') < ?", []any{65}},
		{"JSONLte", JSONLte("data", []string{"age"}, 65), "json_extract(data, '$.age') <= ?", []any{65}},
		{"JSONContains", JSONContains("data", []string{"name"}, "da"), "json_extract(data, '$.name') LIKE ?", []any{"%da%"}},
		{"JSONNotContains", JSONNotContains("data", []string{"name"}, "da"), "json_extract(data, '$.name') NOT LIKE ?", []any{"%da%"}},
		{"JSONStartsWith", JSONStartsWith("data", []string{"name"}, "Ad"), "json_extract(data, '$.name') LIKE ?", []any{"Ad%"}},
		{"JSONEndsWith", JSONEndsWith("data", []string{"name"}, "da"), "json_extract(data, '$.name') LIKE ?", []any{"%da"}},
		{"JSONIsNull", JSONIsNull("data", path), "json_extract(data, '$.address.city') IS NULL", nil},
		{"JSONIsNotNull", JSONIsNotNull("data", path), "json_extract(data, '$.address.city') IS NOT NULL", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, args := compileCondExt(t, tc.cond, Question)
			if s != tc.sql {
				t.Errorf("sql = %q, want %q", s, tc.sql)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args = %v, want %v", args, tc.args)
			}
		})
	}
}

func TestJSONExtPostgres(t *testing.T) {
	s, args := compileCondExt(t, JSONGt("data", []string{"a", "b"}, 1), Dollar)
	if s != "(data #>> '{a,b}') > $1" {
		t.Errorf("sql = %q", s)
	}
	if !reflect.DeepEqual(args, []any{1}) {
		t.Errorf("args = %v", args)
	}
}
