package config

import (
	"fmt"
	"os"
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

// Config holds process configuration (env + defaults).
type Config struct {
	DatabaseURL       string
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
}

func Load() (*Config, error) {
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
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
	return &Config{
		PolyPrivateKey:    strings.TrimSpace(os.Getenv("POLYMARKET_PRIVATE_KEY")),
		PolyAPIKey:        strings.TrimSpace(os.Getenv("POLYMARKET_API_KEY")),
		PolyAPISecret:     strings.TrimSpace(os.Getenv("POLYMARKET_SECRET")),
		PolyAPIPassphrase: strings.TrimSpace(os.Getenv("POLYMARKET_PASSPHRASE")),
		PolyFunderAddress: strings.TrimSpace(os.Getenv("POLYMARKET_FUNDER_ADDRESS")),
		DatabaseURL:       dbURL,
		Host:              host,
		Port:              port,
		PublicPort:        strings.TrimSpace(os.Getenv("PUBLIC_PORT")),
		ReadOnlyMode:      readOnly,
		LogLevel:          strings.TrimSpace(os.Getenv("LOG_LEVEL")),
		CORSOrigins:       origins,
		PolygonRPCURL:     rpc,
		PolymarketAPIURL:  polyAPI,
		PolymarketCLOBWS:  polyWS,
		HTTPPlatformProxy: OutboundProxyURL(),
		TelegramBotToken:  strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramChatID:    strings.TrimSpace(os.Getenv("TELEGRAM_AUTHORIZED_CHAT_ID")),
		ChainID:           chainID,
	}, nil
}
