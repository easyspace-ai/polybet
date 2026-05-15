package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

// LoadEnvFile loads key=value pairs from the first existing file among:
//  1. SPORTS_ROUTER_ENV_FILE (absolute or relative path; e.g. Makefile sets repo .env)
//  2. ~/.polybet/.env (optional local dotenv overrides)
//  3. ".env" next to the executable (packaged binary beside polybet)
//  4. ".env" in the current working directory
//
// Server fields from ~/.polybet/polybet-project.json are applied separately by
// ApplyHomePolybetProjectJSON (only for env vars still empty after this load).
//
// Existing environment variables are not overridden (same as godotenv.Load).
// If no file exists, this is a no-op (suitable for production with only OS env).
//
// Note: the Electron embedded sidecar usually receives DATABASE_URL / HOST / PORT /
// proxy keys directly from the parent process (from polybet-project.json) and clears
// SPORTS_ROUTER_ENV_FILE so this loader does not override those — it only fills keys
// that are still unset (e.g. POLYMARKET_* from a local file).
func LoadEnvFile() {
	candidates := make([]string, 0, 8)
	if p := os.Getenv("SPORTS_ROUTER_ENV_FILE"); p != "" {
		candidates = append(candidates, filepath.Clean(p))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".polybet", ".env"))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	candidates = append(candidates, ".env")

	for _, f := range candidates {
		if f == "" {
			continue
		}
		st, err := os.Stat(f)
		if err != nil || st.IsDir() {
			continue
		}
		if err := godotenv.Load(f); err != nil {
			logrus.WithFields(logx.Pairs("path", f, "err", err)).Warn("环境变量文件：解析失败")
			return
		}
		logrus.WithFields(logx.Pairs("path", filepath.Clean(f))).Info("环境变量文件：已加载")
		return
	}
	logrus.WithFields(logx.Pairs("reason", "no .env file found", "candidates", candidates)).Debug("环境变量文件：未找到，跳过")
}
