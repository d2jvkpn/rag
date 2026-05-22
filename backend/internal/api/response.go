package api

import (
	"github.com/gin-gonic/gin"
)

type fieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func writeData(c *gin.Context, status int, data any) {
	c.JSON(status, map[string]any{
		"data": data,
	})
}

func writeError(c *gin.Context, status int, code, message string, details []fieldError) {
	payload := map[string]any{
		"code":    code,
		"message": message,
	}
	if len(details) > 0 {
		payload["details"] = details
	}
	c.JSON(status, map[string]any{
		"error": payload,
	})
}
