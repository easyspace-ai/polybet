package gammaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOppositeCLOBTokenID_binary(t *testing.T) {
	const held = "0x1111111111111111111111111111111111111111111111111111111111111111"
	const other = "0x2222222222222222222222222222222222222222222222222222222222222222"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"question":"Q","clobTokenIds":["` + held + `","` + other + `"]}]`))
	}))
	defer srv.Close()

	orig := gammaAPIBase
	gammaAPIBase = srv.URL
	defer func() { gammaAPIBase = orig }()

	got, err := OppositeCLOBTokenID(context.Background(), "", held)
	if err != nil {
		t.Fatal(err)
	}
	if got != other {
		t.Fatalf("got %q want %q", got, other)
	}
}

func TestOppositeCLOBTokenID_notBinary(t *testing.T) {
	const a = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"question":"Q","clobTokenIds":["` + a + `","b","c"]}]`))
	}))
	defer srv.Close()
	orig := gammaAPIBase
	gammaAPIBase = srv.URL
	defer func() { gammaAPIBase = orig }()

	_, err := OppositeCLOBTokenID(context.Background(), "", a)
	if err == nil {
		t.Fatal("expected error for non-binary market")
	}
}
