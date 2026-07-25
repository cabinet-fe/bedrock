package pkg

import "strings"

// OrderBy resolves ProTable sort query "field@asc|desc" against an allowlist.
// allowed maps API field name → SQL column (may include table prefix).
// idColumn is the stable tie-breaker (e.g. "id" or "product_projects.id").
// Empty / unknown / invalid sort falls back to defaultOrder.
func OrderBy(sort string, allowed map[string]string, idColumn, defaultOrder string) string {
	sort = strings.TrimSpace(sort)
	if sort == "" || idColumn == "" {
		return defaultOrder
	}
	at := strings.LastIndex(sort, "@")
	if at <= 0 || at >= len(sort)-1 {
		return defaultOrder
	}
	field := sort[:at]
	dir := strings.ToLower(sort[at+1:])
	col, ok := allowed[field]
	if !ok || col == "" {
		return defaultOrder
	}
	switch dir {
	case "asc":
		return col + " ASC, " + idColumn + " ASC"
	case "desc":
		return col + " DESC, " + idColumn + " DESC"
	default:
		return defaultOrder
	}
}
