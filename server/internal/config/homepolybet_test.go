package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyHomePolybetProjectJSONFillsOnlyEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)

	_ = os.Unsetenv("POLYBET_BADGER_DIR")
	_ = os.Unsetenv("HOST")
	_ = os.Unsetenv("PORT")
	_ = os.Unsetenv("HTTP_PLATFORM_PROXY_URL")
	_ = os.Unsetenv("READ_ONLY_MODE")
	_ = os.Unsetenv("LOG_LEVEL")

	pb := filepath.Join(home, ".polybet")
	if err := os.MkdirAll(pb, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "schemaVersion": 1,
  "badgerDir": "/tmp/polybet-badger-test",
  "host": "127.0.0.1",
  "port": 7644,
  "outboundProxyUrl": "http://127.0.0.1:1",
  "readOnlyMode": false,
  "logLevel": "debug"
}`
	if err := os.WriteFile(filepath.Join(pb, "polybet-project.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	ApplyHomePolybetProjectJSON()

	if got := os.Getenv("POLYBET_BADGER_DIR"); got != "/tmp/polybet-badger-test" {
		t.Fatalf("POLYBET_BADGER_DIR: got %q", got)
	}
	if got := os.Getenv("HOST"); got != "127.0.0.1" {
		t.Fatalf("HOST: got %q", got)
	}
	if got := os.Getenv("PORT"); got != "7644" {
		t.Fatalf("PORT: got %q", got)
	}
	if got := os.Getenv("HTTP_PLATFORM_PROXY_URL"); got != "http://127.0.0.1:1" {
		t.Fatalf("HTTP_PLATFORM_PROXY_URL: got %q", got)
	}
	if got := os.Getenv("READ_ONLY_MODE"); got != "false" {
		t.Fatalf("READ_ONLY_MODE: got %q", got)
	}
	if got := os.Getenv("LOG_LEVEL"); got != "debug" {
		t.Fatalf("LOG_LEVEL: got %q", got)
	}

	_ = os.Setenv("POLYBET_BADGER_DIR", "preset")
	ApplyHomePolybetProjectJSON()
	if got := os.Getenv("POLYBET_BADGER_DIR"); got != "preset" {
		t.Fatalf("POLYBET_BADGER_DIR should stay preset, got %q", got)
	}
}
