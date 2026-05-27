package api

import (
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (h *Handler) cors() gin.HandlerFunc {
	allowed, allowAny := parseAllowOrigins(h.cfg.GetStringSlice("http.allow_origins"))
	return cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			origin = strings.TrimSpace(origin)
			return origin != "" && (allowAny || allowed[origin])
		},
		AllowMethods: []string{
			httpMethodGet,
			httpMethodPost,
			httpMethodPut,
			httpMethodDelete,
			httpMethodOptions,
		},
		AllowHeaders: []string{
			"Authorization",
			"Content-Type",
			"X-Requested-With",
		},
		AllowCredentials: true,
	})
}

func parseAllowOrigins(values []string) (map[string]bool, bool) {
	allowed := make(map[string]bool, len(values))
	allowAny := false
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
	return allowed, allowAny
}

const (
	httpMethodGet     = "GET"
	httpMethodPost    = "POST"
	httpMethodPut     = "PUT"
	httpMethodDelete  = "DELETE"
	httpMethodOptions = "OPTIONS"
)
