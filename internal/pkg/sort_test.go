package pkg

import "testing"

func TestOrderBy(t *testing.T) {
	allowed := map[string]string{
		"name":       "product_projects.name",
		"updated_at": "product_projects.updated_at",
	}
	const (
		idCol = "product_projects.id"
		def   = "product_projects.updated_at DESC, product_projects.id DESC"
	)

	tests := []struct {
		sort string
		want string
	}{
		{"", def},
		{"name@asc", "product_projects.name ASC, product_projects.id ASC"},
		{"name@desc", "product_projects.name DESC, product_projects.id DESC"},
		{"updated_at@asc", "product_projects.updated_at ASC, product_projects.id ASC"},
		{"bogus@desc", def},
		{"name@sideways", def},
		{"@asc", def},
		{"name@", def},
	}
	for _, tt := range tests {
		if got := OrderBy(tt.sort, allowed, idCol, def); got != tt.want {
			t.Fatalf("OrderBy(%q) = %q, want %q", tt.sort, got, tt.want)
		}
	}
}
