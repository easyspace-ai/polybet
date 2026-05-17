package httpserver

import (
	"net/http/pprof"

	"github.com/gin-gonic/gin"
)

// registerPprof mounts stdlib pprof handlers under /debug/pprof when enabled.
// Use POLYBET_ENABLE_PPROF=true (see config). Capture CPU during slow refresh:
//
//	go tool pprof http://127.0.0.1:7633/debug/pprof/profile?seconds=30
func registerPprof(r *gin.Engine) {
	pr := r.Group("/debug/pprof")
	pr.GET("/", gin.WrapF(pprof.Index))
	pr.GET("/cmdline", gin.WrapF(pprof.Cmdline))
	pr.GET("/profile", gin.WrapF(pprof.Profile))
	pr.GET("/symbol", gin.WrapF(pprof.Symbol))
	pr.POST("/symbol", gin.WrapF(pprof.Symbol))
	pr.GET("/trace", gin.WrapF(pprof.Trace))
	pr.GET("/allocs", gin.WrapH(pprof.Handler("allocs")))
	pr.GET("/block", gin.WrapH(pprof.Handler("block")))
	pr.GET("/goroutine", gin.WrapH(pprof.Handler("goroutine")))
	pr.GET("/heap", gin.WrapH(pprof.Handler("heap")))
	pr.GET("/mutex", gin.WrapH(pprof.Handler("mutex")))
	pr.GET("/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))
}
