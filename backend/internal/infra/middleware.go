package infra

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func RequestLogger() gin.HandlerFunc {
	log := L.WithOptions(zap.WithCaller(false))
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		fields := []zap.Field{
			zap.String("ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", time.Since(start)),
		}

		if len(c.Params) > 0 {
			params := make(map[string]string, len(c.Params))
			for _, p := range c.Params {
				params[p.Key] = p.Value
			}
			fields = append(fields, zap.Any("params", params))
		}
		if raw := c.Request.URL.RawQuery; raw != "" {
			fields = append(fields, zap.String("query", raw))
		}
		if v, ok := c.Get("err_origin"); ok {
			fields = append(fields, zap.String("err_origin", v.(string)))
		}
		if v, ok := c.Get("err_code"); ok {
			fields = append(fields, zap.String("err_code", v.(string)))
		}
		if v, ok := c.Get("err_message"); ok {
			fields = append(fields, zap.String("err_message", v.(string)))
		}

		switch {
		case status >= 500:
			log.Error("request", fields...)
		case status >= 400:
			log.Warn("request", fields...)
		default:
			log.Info("request", fields...)
		}
	}
}
