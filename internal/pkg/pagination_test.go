package pkg

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParsePage_defaultsAndCaps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	q := ParsePage(c)
	if q.Page != 1 || q.PageSize != 20 {
		t.Fatalf("defaults: %+v", q)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/x?page=0&page_size=500", nil)
	q2 := ParsePage(c2)
	if q2.Page != 1 || q2.PageSize != 100 {
		t.Fatalf("caps: %+v", q2)
	}
}

func TestNewPageResult(t *testing.T) {
	r := NewPageResult([]string{"a"}, 25, PageQuery{Page: 2, PageSize: 10})
	if r.TotalPages != 3 || r.Page != 2 || r.Total != 25 {
		t.Fatalf("%+v", r)
	}
}
