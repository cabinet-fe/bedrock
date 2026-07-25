package pkg

import (
	"math"
	"reflect"

	"github.com/gin-gonic/gin"
)

// PageQuery holds normalized pagination fields for response envelopes.
type PageQuery struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// ListQuery is the shared list-query input: page / page_size / sort.
type ListQuery struct {
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	Sort     string `form:"sort"`
}

// PageResult is the standard list envelope: items/total/page/page_size/total_pages.
type PageResult struct {
	Items      interface{} `json:"items"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// PaginatedData is an alias kept for older call sites.
type PaginatedData = PageResult

// NewPageResult builds a pagination envelope.
func NewPageResult(items interface{}, total int64, page PageQuery) PageResult {
	totalPages := 0
	if page.PageSize > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(page.PageSize)))
	}
	if items == nil {
		items = []interface{}{}
	}
	return PageResult{
		Items:      items,
		Total:      total,
		Page:       page.Page,
		PageSize:   page.PageSize,
		TotalPages: totalPages,
	}
}

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// Normalize applies defaults and caps for page / page_size.
func (q ListQuery) Normalize() ListQuery {
	if q.Page < 1 {
		q.Page = defaultPage
	}
	if q.PageSize < 1 {
		q.PageSize = defaultPageSize
	}
	if q.PageSize > maxPageSize {
		q.PageSize = maxPageSize
	}
	return q
}

// Offset is the SQL OFFSET for the current page.
func (q ListQuery) Offset() int {
	return (q.Page - 1) * q.PageSize
}

// ParseListQuery reads page / page_size / sort from the query string and normalizes.
func ParseListQuery(c *gin.Context) ListQuery {
	var q ListQuery
	_ = c.ShouldBindQuery(&q)
	return q.Normalize()
}

// BindList binds domain filter fields (and embedded ListQuery) from the query,
// normalizes the embedded ListQuery in place, and returns it.
func BindList(c *gin.Context, dst any) ListQuery {
	_ = c.ShouldBindQuery(dst)
	return normalizeEmbeddedListQuery(dst)
}

func normalizeEmbeddedListQuery(dst any) ListQuery {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ListQuery{}.Normalize()
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return ListQuery{}.Normalize()
	}
	f := v.FieldByName("ListQuery")
	if !f.IsValid() || !f.CanSet() || f.Type() != reflect.TypeOf(ListQuery{}) {
		return ListQuery{}.Normalize()
	}
	q := f.Interface().(ListQuery).Normalize()
	f.Set(reflect.ValueOf(q))
	return q
}

// PageSuccess writes a success response with the standard pagination envelope as data.
func PageSuccess(c *gin.Context, items interface{}, total int64, q ListQuery) {
	Success(c, NewPageResult(items, total, PageQuery{Page: q.Page, PageSize: q.PageSize}))
}

// Paginated keeps the 1.x helper signature.
func Paginated(c *gin.Context, items interface{}, total int64, page, pageSize int) {
	PageSuccess(c, items, total, ListQuery{Page: page, PageSize: pageSize})
}
