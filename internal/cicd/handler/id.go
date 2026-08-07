package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
)

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	return uint(id), err
}

// parseOptionalUintQuery parses an optional positive uint query param.
// Empty → (nil, nil); invalid → error.
func parseOptionalUintQuery(c *gin.Context, key string) (*uint, error) {
	v := c.Query(key)
	if v == "" {
		return nil, nil
	}
	id, err := strconv.ParseUint(v, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	u := uint(id)
	return &u, nil
}
