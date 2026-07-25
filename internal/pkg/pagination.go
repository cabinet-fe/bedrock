package pkg

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"
)

// PageQuery holds normalized pagination query params.
type PageQuery struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
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

// ParsePage reads page/page_size from query with defaults and caps.
func ParsePage(c *gin.Context) PageQuery {
	page := defaultPage
	pageSize := defaultPageSize
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil {
			pageSize = v
		}
	}
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return PageQuery{Page: page, PageSize: pageSize}
}

// PageSuccess writes a success response with the standard pagination envelope as data.
func PageSuccess(c *gin.Context, items interface{}, total int64, page PageQuery) {
	Success(c, NewPageResult(items, total, page))
}

// Paginated keeps the 1.x helper signature.
func Paginated(c *gin.Context, items interface{}, total int64, page, pageSize int) {
	PageSuccess(c, items, total, PageQuery{Page: page, PageSize: pageSize})
}
