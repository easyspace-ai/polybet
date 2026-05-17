package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Writer.Header().Set("X-Request-ID", id)
		c.Set("request_id", id)
		c.Next()
	}
}

func accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.Request.URL.Path
		if path == "" {
			path = "/"
		}
		latency := time.Since(start)
		status := c.Writer.Status()
		rid := c.GetString("request_id")
		logx.LogHTTPAccess(
			c.Request.Method,
			path,
			status,
			latency,
			c.ClientIP(),
			rid,
		)
		// Also emit to stdout so dev consoles show API traffic (disk log may be disabled).
		if shouldLogHTTPAccessToConsole(path) {
			logrus.WithFields(logx.Pairs(
				"request_id", rid,
				"method", c.Request.Method,
				"path", path,
				"status", status,
				"latency_ms", float64(latency.Microseconds())/1000.0,
				"client_ip", c.ClientIP(),
			)).Info("HTTP")
		}
	}
}

func shouldLogHTTPAccessToConsole(path string) bool {
	if path == "/api/health" {
		return false
	}
	if strings.HasPrefix(path, "/assets/") || path == "/favicon.ico" {
		return false
	}
	return strings.HasPrefix(path, "/api/")
}

func cors(origins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		o := "*"
		if len(origins) > 0 {
			o = strings.Join(origins, ",")
		}
		c.Writer.Header().Set("Access-Control-Allow-Origin", o)
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
