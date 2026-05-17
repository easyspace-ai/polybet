package polywarm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBookJSONHTTPOK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty_sides_200", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/book" {
				t.Fatalf("path %q", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bids":[],"asks":[]}`))
		}))
		defer srv.Close()
		if !BookJSONHTTPOK(ctx, srv.URL, "", "0xf253fcb8e0864b570575cf7d14c9621151c1e371490939f65d3f25847f84f240") {
			t.Fatal("expected true for 200 + decodable book")
		}
	})

	t.Run("404", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if BookJSONHTTPOK(ctx, srv.URL, "", "any") {
			t.Fatal("expected false for 404")
		}
	})
}
