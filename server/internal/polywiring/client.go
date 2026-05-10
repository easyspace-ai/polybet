package polywiring

import (
	"fmt"
	polymarket "github.com/easyspace-ai/polysdk"
	"strings"

	"github.com/easyspace-ai/polysdk/pkg/auth"
	"github.com/easyspace-ai/polysdk/pkg/clob"
	"github.com/ethereum/go-ethereum/common"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/store"
)

// AuthedCLOB bundles an authenticated CLOB client with its signer (for OrderBuilder)
// and the L2 API key (needed for authenticated CLOB WebSocket user streams).
type AuthedCLOB struct {
	Client clob.Client
	Signer auth.Signer
	APIKey *auth.APIKey
}

func BuildAuthedCLOB(cfg *config.Config, acct *store.PolymarketAccount) (*AuthedCLOB, error) {
	pk := strings.TrimSpace(acct.PrivateKey)
	if pk == "" {
		return nil, fmt.Errorf("empty private key")
	}
	signer, err := auth.NewPrivateKeySigner(pk, cfg.ChainID)
	if err != nil {
		return nil, err
	}
	apiKey := &auth.APIKey{
		Key:        acct.APIKey,
		Secret:     acct.Secret,
		Passphrase: acct.Passphrase,
	}
	pc := polymarket.DefaultConfig()
	pc.BaseURLs.CLOB = cfg.PolymarketAPIURL
	if cfg.PolymarketCLOBWS != "" {
		pc.BaseURLs.CLOBWS = cfg.PolymarketCLOBWS
	}
	opts := []polymarket.Option{
		polymarket.WithConfig(pc),
		polymarket.WithUseServerTime(true),
	}
	if cfg.HTTPPlatformProxy != "" {
		opts = append(opts, polymarket.WithProxyURL(cfg.HTTPPlatformProxy))
	}
	root := polymarket.NewClient(opts...)
	funder := common.HexToAddress(strings.TrimSpace(acct.FunderAddress))
	cl := root.CLOB.WithAuth(signer, apiKey).
		WithSignatureType(auth.SignaturePoly1271).
		WithFunder(funder)
	return &AuthedCLOB{Client: cl, Signer: signer, APIKey: apiKey}, nil
}
