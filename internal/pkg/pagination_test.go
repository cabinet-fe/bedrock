package pkg

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseListQuery_defaultsAndCaps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	q := ParseListQuery(c)
	if q.Page != 1 || q.PageSize != 20 || q.Sort != "" {
		t.Fatalf("defaults: %+v", q)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/x?page=0&page_size=500&sort=name@asc", nil)
	q2 := ParseListQuery(c2)
	if q2.Page != 1 || q2.PageSize != 100 || q2.Sort != "name@asc" {
		t.Fatalf("caps: %+v", q2)
	}
}

func TestBindList_embedsAndNormalizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type filter struct {
		ListQuery
		Keyword string `form:"keyword"`
		Status  string `form:"status"`
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x?page=2&page_size=10&sort=updated_at@desc&keyword=foo&status=active", nil)

	var f filter
	q := BindList(c, &f)
	if q.Page != 2 || q.PageSize != 10 || q.Sort != "updated_at@desc" {
		t.Fatalf("list query: %+v", q)
	}
	if f.Page != 2 || f.PageSize != 10 || f.Sort != "updated_at@desc" {
		t.Fatalf("embedded: %+v", f.ListQuery)
	}
	if f.Keyword != "foo" || f.Status != "active" {
		t.Fatalf("domain: %+v", f)
	}
	if q.Offset() != 10 {
		t.Fatalf("Offset: %d", q.Offset())
	}
}

func TestNewPageResult(t *testing.T) {
	r := NewPageResult([]string{"a"}, 25, PageQuery{Page: 2, PageSize: 10})
	if r.TotalPages != 3 || r.Page != 2 || r.Total != 25 {
		t.Fatalf("%+v", r)
	}
}
