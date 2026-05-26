package api

import (
	"fmt"
	"runtime"
	"strings"

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
	if _, file, line, ok := runtime.Caller(1); ok {
		c.Set("err_origin", fmt.Sprintf("%s:%d", trimSourcePath(file), line))
	}
	c.Set("err_code", code)
	c.Set("err_message", message)

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

func trimSourcePath(file string) string {
	if i := strings.Index(file, "/backend/"); i >= 0 {
		return file[i+len("/backend/"):]
	}
	return file
}
