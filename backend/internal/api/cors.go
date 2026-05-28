package api

import (
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (h *Handler) cors() gin.HandlerFunc {
	var (
		values   []string
		allowed  map[string]bool
		allowAny bool
	)

	values = h.cfg.GetStringSlice("http.allow_origins")
	allowed = make(map[string]bool, len(values))

	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			origin := strings.TrimSpace(part)
			if origin == "" {
				continue
			}
			if origin == "*" {
				allowAny = true
				continue
			}
			allowed[origin] = true
		}
	}

	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			origin = strings.TrimSpace(origin)
			return origin != "" && (allowAny || allowed[origin])
		},
		AllowMethods: []string{
			"GET",
			"POST",
			"PUT",
			"DELETE",
			"OPTIONS",
		},
		AllowHeaders: []string{
			"Authorization",
			"Content-Type",
			"X-Requested-With",
		},
		AllowCredentials: true,
	})
}
