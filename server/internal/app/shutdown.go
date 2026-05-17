package app

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
)

const shutdownClearSubsTimeout = 2 * time.Second

// beginShutdown stops inbound work that blocks http.Server.Shutdown (dashboard WS
// read loops) and tears down upstream Polymarket streams.
func (a *App) beginShutdown() {
	a.Log.Info("优雅关闭：断开客户端 WebSocket 与上游连接")
	if a.Hub != nil {
		a.Hub.CloseAll()
	}
	if a.RiskHub != nil {
		a.RiskHub.CloseAll()
	}
	a.userStreamMu.Lock()
	user := a.activeUserWS
	a.userStreamMu.Unlock()
	if user != nil {
		user.Stop()
	}
	if a.StopLoss != nil {
		a.clearStopLossSubscriptionsBounded()
		a.StopLoss.Shutdown()
	}
	if a.RiskRuntime != nil {
		a.RiskRuntime.CloseJSONLLog()
	}
}

// clearStopLossSubscriptionsBounded runs clearSubscriptions with a short cap so
// Run() is not stuck on upstream unsubscribe frames during exit.
func (a *App) clearStopLossSubscriptionsBounded() {
	if a.StopLoss == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		a.StopLoss.ClearSubscriptions()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownClearSubsTimeout):
		a.Log.Warn("优雅关闭：止损订阅清理超时，继续退出")
	}
}

func (a *App) shutdownHTTPServer(srv *http.Server, label string) {
	if srv == nil {
		return
	}
	shCtx, cancel := context.WithTimeout(context.Background(), shutdownHTTPTimeout)
	defer cancel()
	if err := srv.Shutdown(shCtx); err != nil {
		a.Log.WithFields(logx.Pairs("server", label, "err", err.Error())).Warn("HTTP Shutdown 超时，强制 Close")
		_ = srv.Close()
	}
}

func serverBaseContext(ctx context.Context) func(net.Listener) context.Context {
	return func(net.Listener) context.Context {
		return ctx
	}
}
