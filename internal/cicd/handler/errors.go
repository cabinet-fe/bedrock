package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"bedrock/internal/cicd/service"
	"bedrock/internal/pkg"
)

func writeServiceError(c *gin.Context, err error) {
	switch {
	case service.IsConflict(err):
		pkg.Error(c, http.StatusConflict, err.Error())
	case service.IsForbidden(err):
		pkg.Error(c, http.StatusForbidden, err.Error())
	case service.IsNotFound(err):
		pkg.Error(c, http.StatusNotFound, err.Error())
	default:
		pkg.Error(c, http.StatusBadRequest, err.Error())
	}
}
