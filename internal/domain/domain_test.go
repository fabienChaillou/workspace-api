package domain

import (
	"errors"
	"testing"
)

func TestEnvVarsRoundTrip(t *testing.T) {
	in := []EnvVars{{Name: "A", Value: "1"}, {Name: "B", Value: "2", Secure: true, Hidden: true}}
	raw, err := EncodeEnvVars(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"A","value":"1","secure":false,"hidden":false},{"name":"B","value":"2","secure":true,"hidden":true}]`
	if raw != want {
		t.Fatalf("encoded = %s", raw)
	}
	out, err := DecodeEnvVars(raw)
	if err != nil || len(out) != 2 || out[1] != in[1] {
		t.Fatalf("decoded = %+v, %v", out, err)
	}
}

func TestEnvVarsEmpty(t *testing.T) {
	raw, _ := EncodeEnvVars(nil)
	if raw != "[]" {
		t.Fatalf("nil encodes to %s", raw)
	}
	for _, in := range []string{"", "  ", "null", "[]"} {
		out, err := DecodeEnvVars(in)
		if err != nil || out == nil || len(out) != 0 {
			t.Fatalf("decode(%q) = %#v, %v", in, out, err)
		}
	}
	if _, err := DecodeEnvVars("{not json"); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestValidateEnvVars(t *testing.T) {
	if err := ValidateEnvVars([]EnvVars{{Name: "A"}, {Name: "B"}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]EnvVars{{{Name: ""}}, {{Name: " "}}, {{Name: "A"}, {Name: "A"}}} {
		if err := ValidateEnvVars(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ValidateEnvVars(%+v) = %v", bad, err)
		}
	}
}

func TestEnums(t *testing.T) {
	for _, s := range []WorkspaceStatus{"ACTIVE", "FAILURE", "INACTIVE", "DELETED"} {
		if !s.Valid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	for _, s := range []WorkspaceStatus{"", "active", "FAILLURE"} {
		if s.Valid() {
			t.Fatalf("%q should be invalid", s)
		}
	}
	for _, a := range []ActionType{"init", "plan", "apply", "destroy"} {
		if !a.Valid() {
			t.Fatalf("%s should be valid", a)
		}
	}
	if ActionType("refresh").Valid() || ActionType("PLAN").Valid() {
		t.Fatal("unexpected valid action type")
	}
}

func TestNormalizeDefaults(t *testing.T) {
	o, err := ListWorkspacesOptions{Filter: QueryParams{Name: "  net ", Namespace: " ns "}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if o.Sort != SortByActionsUpdatedAt || o.Order != SortDesc {
		t.Fatalf("sort defaults = %s %s", o.Sort, o.Order)
	}
	if o.Pagination.Page != 1 || o.Pagination.Limit != 10 || o.Pagination.Offset() != 0 {
		t.Fatalf("pagination defaults = %+v", o.Pagination)
	}
	if o.Filter.Name != "net" || o.Filter.Namespace != "ns" {
		t.Fatalf("filters not trimmed: %+v", o.Filter)
	}
}

func TestNormalizeErrors(t *testing.T) {
	cases := []ListWorkspacesOptions{
		{Filter: QueryParams{Status: "BROKEN"}},
		{Filter: QueryParams{ActionType: "refresh"}},
		{Sort: "vcs_url"},
		{Order: "up"},
		{Pagination: Pagination{Page: -1}},
		{Pagination: Pagination{Limit: -1}},
		{Pagination: Pagination{Limit: MaxLimit + 1}},
	}
	for _, c := range cases {
		if _, err := c.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Normalize(%+v) = %v, want ErrInvalid", c, err)
		}
	}
}

func TestPagination(t *testing.T) {
	cases := []struct {
		limit int
		total int64
		pages int
	}{{10, 0, 0}, {10, 1, 1}, {10, 10, 1}, {10, 11, 2}, {3, 10, 4}}
	for _, c := range cases {
		p := Pagination{Page: 1, Limit: c.limit}.WithTotal(c.total)
		if p.TotalPages != c.pages || p.TotalRows != c.total {
			t.Fatalf("limit=%d total=%d -> %+v, want %d pages", c.limit, c.total, p, c.pages)
		}
	}
	if off := (Pagination{Page: 3, Limit: 10}).Offset(); off != 20 {
		t.Fatalf("offset = %d", off)
	}
}
