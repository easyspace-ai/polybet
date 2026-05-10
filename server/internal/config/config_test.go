package config

import (
	"os"
	"testing"
)

func TestOutboundProxyURL_precedence(t *testing.T) {
	t.Cleanup(func() {
		for _, k := range []string{
			"HTTP_PLATFORM_PROXY_URL", "ALL_PROXY", "all_proxy",
			"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy",
		} {
			_ = os.Unsetenv(k)
		}
	})

	t.Setenv("HTTPS_PROXY", "http://https-only:1")
	t.Setenv("HTTP_PLATFORM_PROXY_URL", "http://platform:2")
	if got := OutboundProxyURL(); got != "http://platform:2" {
		t.Fatalf("expected HTTP_PLATFORM_PROXY_URL to win, got %q", got)
	}

	_ = os.Unsetenv("HTTP_PLATFORM_PROXY_URL")
	t.Setenv("ALL_PROXY", "socks5://all:3")
	t.Setenv("HTTPS_PROXY", "http://https-only:1")
	if got := OutboundProxyURL(); got != "socks5://all:3" {
		t.Fatalf("expected ALL_PROXY before HTTPS_PROXY, got %q", got)
	}

	_ = os.Unsetenv("ALL_PROXY")
	if got := OutboundProxyURL(); got != "http://https-only:1" {
		t.Fatalf("expected HTTPS_PROXY, got %q", got)
	}

	_ = os.Unsetenv("HTTPS_PROXY")
	t.Setenv("http_proxy", "http://lower:4")
	if got := OutboundProxyURL(); got != "http://lower:4" {
		t.Fatalf("expected http_proxy, got %q", got)
	}

	_ = os.Unsetenv("http_proxy")
	if got := OutboundProxyURL(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
