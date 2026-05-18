package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
)

func TestHandleRiskBook_requiresTokenId(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{cache: bookcache.New(5)}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/risk/book", nil)
	h.handleRiskBook(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRiskBook_returnsCacheShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid := "0x000000000000000000000000000000000000000000000000000000000000abc1"
	cache := bookcache.New(5)
	cache.ReplaceBook(tid, []struct{ Price, Size string }{
		{Price: "0.45", Size: "10"},
	}, nil, time.Now().UnixMilli())

	h := &Handler{
		cfg:   &config.Config{PolymarketAPIURL: "https://clob.polymarket.com"},
		cache: cache,
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/risk/book?tokenId="+tid, nil)
	h.handleRiskBook(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["tokenId"] != tid {
		t.Fatalf("tokenId=%v", body["tokenId"])
	}
	if body["source"] != "cache" {
		t.Fatalf("source=%v", body["source"])
	}
}
