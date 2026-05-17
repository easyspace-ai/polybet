package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OutboundProxyURL returns the first non-empty proxy URL used for Polymarket
// (Gamma HTTP, CLOB REST/WS, SDK). Order: app-specific env, then common shell
// variables so HTTPS_PROXY / ALL_PROXY work without HTTP_PLATFORM_PROXY_URL.
func OutboundProxyURL() string {
	for _, k := range []string{
		"HTTP_PLATFORM_PROXY_URL",
		"ALL_PROXY", "all_proxy",
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
	} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// DefaultMarketsSyncIntervalMin is the Gamma/event-list sync interval when
// bot_config pollingInterval is unset (60 minutes = 1 hour).
const DefaultMarketsSyncIntervalMin = 60

// Config holds process configuration (env + defaults).
type Config struct {
	Host              string
	Port              string
	PublicPort        string
	ReadOnlyMode      bool
	LogLevel          string
	CORSOrigins       []string
	PolygonRPCURL     string
	PolymarketAPIURL  string
	PolymarketCLOBWS  string
	HTTPPlatformProxy string
	TelegramBotToken  string
	TelegramChatID    string
	ChainID           int64
	PolyPrivateKey    string
	PolyAPIKey        string
	PolyAPISecret     string
	PolyAPIPassphrase string
	PolyFunderAddress string
	BadgerDir         string
	BadgerSyncWrites  bool
	// EnablePprof exposes /debug/pprof on the main HTTP server (POLYBET_ENABLE_PPROF=true).
	EnablePprof bool
}

func Load() (*Config, error) {
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "7633"
	}
	chainID := int64(137)
	if s := strings.TrimSpace(os.Getenv("POLYMARKET_CHAIN_ID")); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			chainID = v
		}
	}
	readOnly := strings.EqualFold(strings.TrimSpace(os.Getenv("READ_ONLY_MODE")), "true")
	logLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if logLevel == "" {
		// Quieter default: fewer console/disk lines; set LOG_LEVEL=info or debug for verbose ops.
		logLevel = "warn"
	}
	cors := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	var origins []string
	if cors != "" {
		for _, p := range strings.Split(cors, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				origins = append(origins, p)
			}
		}
	}
	polyAPI := strings.TrimSpace(os.Getenv("POLYMARKET_API_URL"))
	if polyAPI == "" {
		polyAPI = "https://clob.polymarket.com"
	}
	polyWS := strings.TrimSpace(os.Getenv("POLYMARKET_CLOB_WS_URL"))
	rpc := strings.TrimSpace(os.Getenv("POLYGON_RPC_URL"))
	if rpc == "" {
		rpc = "https://polygon-rpc.com"
	}
	badgerDir := strings.TrimSpace(os.Getenv("POLYBET_BADGER_DIR"))
	if badgerDir == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			badgerDir = filepath.Join(h, ".polybet", "badger")
		}
	}
	if badgerDir == "" {
		return nil, fmt.Errorf("POLYBET_BADGER_DIR is required (or a writable home directory for default ~/.polybet/badger)")
	}
	badgerSyncWrites := true
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("POLYBET_BADGER_SYNC_WRITES"))); v == "false" || v == "0" {
		badgerSyncWrites = false
	}
	enablePprof := strings.EqualFold(strings.TrimSpace(os.Getenv("POLYBET_ENABLE_PPROF")), "true")
	return &Config{
		PolyPrivateKey:    strings.TrimSpace(os.Getenv("POLYMARKET_PRIVATE_KEY")),
		PolyAPIKey:        strings.TrimSpace(os.Getenv("POLYMARKET_API_KEY")),
		PolyAPISecret:     strings.TrimSpace(os.Getenv("POLYMARKET_SECRET")),
		PolyAPIPassphrase: strings.TrimSpace(os.Getenv("POLYMARKET_PASSPHRASE")),
		PolyFunderAddress: strings.TrimSpace(os.Getenv("POLYMARKET_FUNDER_ADDRESS")),
		Host:              host,
		Port:              port,
		PublicPort:        strings.TrimSpace(os.Getenv("PUBLIC_PORT")),
		ReadOnlyMode:      readOnly,
		LogLevel:          logLevel,
		CORSOrigins:       origins,
		PolygonRPCURL:     rpc,
		PolymarketAPIURL:  polyAPI,
		PolymarketCLOBWS:  polyWS,
		HTTPPlatformProxy: OutboundProxyURL(),
		TelegramBotToken:  strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramChatID:    strings.TrimSpace(os.Getenv("TELEGRAM_AUTHORIZED_CHAT_ID")),
		ChainID:           chainID,
		BadgerDir:         badgerDir,
		BadgerSyncWrites:  badgerSyncWrites,
		EnablePprof:       enablePprof,
	}, nil
}
