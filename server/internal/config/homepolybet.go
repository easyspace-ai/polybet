package config

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ApplyHomePolybetProjectJSON reads ~/.polybet/polybet-project.json and sets
// DATABASE_URL, HOST, PORT, HTTP_PLATFORM_PROXY_URL, READ_ONLY_MODE, and
// LOG_LEVEL only when each env var is still empty (Electron / godotenv wins).
func ApplyHomePolybetProjectJSON() {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	p := filepath.Join(home, ".polybet", "polybet-project.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		slog.Warn("polybet_project_json_invalid", "path", p, "err", err)
		return
	}

	setIfEmpty := func(key, val string) {
		if strings.TrimSpace(os.Getenv(key)) != "" || strings.TrimSpace(val) == "" {
			return
		}
		_ = os.Setenv(key, val)
	}

	var s string
	if raw, ok := root["databaseUrl"]; ok {
		if json.Unmarshal(raw, &s) == nil {
			setIfEmpty("DATABASE_URL", strings.TrimSpace(s))
		}
	}
	if raw, ok := root["host"]; ok {
		s = ""
		if json.Unmarshal(raw, &s) == nil {
			setIfEmpty("HOST", strings.TrimSpace(s))
		}
	}
	if raw, ok := root["port"]; ok {
		if ps, ok := portFromJSON(raw); ok {
			setIfEmpty("PORT", ps)
		}
	}
	if raw, ok := root["outboundProxyUrl"]; ok {
		s = ""
		if json.Unmarshal(raw, &s) == nil {
			setIfEmpty("HTTP_PLATFORM_PROXY_URL", strings.TrimSpace(s))
		}
	}
	if raw, ok := root["readOnlyMode"]; ok {
		if strings.TrimSpace(os.Getenv("READ_ONLY_MODE")) != "" {
			// leave as-is
		} else {
			var b bool
			if json.Unmarshal(raw, &b) == nil {
				if b {
					_ = os.Setenv("READ_ONLY_MODE", "true")
				} else {
					_ = os.Setenv("READ_ONLY_MODE", "false")
				}
			}
		}
	}
	if raw, ok := root["logLevel"]; ok {
		s = ""
		if json.Unmarshal(raw, &s) == nil {
			setIfEmpty("LOG_LEVEL", strings.TrimSpace(s))
		}
	}
	slog.Debug("polybet_project_json_applied_if_needed", "path", p)
}

func portFromJSON(raw json.RawMessage) (string, bool) {
	var str string
	if json.Unmarshal(raw, &str) == nil && strings.TrimSpace(str) != "" {
		return strings.TrimSpace(str), true
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return strconv.FormatInt(n, 10), true
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return strconv.FormatInt(int64(f), 10), true
	}
	return "", false
}
