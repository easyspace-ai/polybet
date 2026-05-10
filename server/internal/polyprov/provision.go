package polyprov

import (
	"context"
	"fmt"
	polymarket "github.com/easyspace-ai/polysdk"
	"strings"

	"github.com/easyspace-ai/polysdk/pkg/auth"
	"github.com/ethereum/go-ethereum/common"

	"github.com/easyspace-ai/polybet/internal/config"
)

type Creds struct {
	FunderAddress string
	APIKey        string
	Secret        string
	Passphrase    string
}

// FromPrivateKey derives Polymarket CREATE2 deposit-wallet funder + L2 API credentials (matches Node `derivePolymarketDepositWalletAddress`).
func FromPrivateKey(ctx context.Context, cfg *config.Config, privateKeyHex string) (*Creds, error) {
	pk := strings.TrimSpace(privateKeyHex)
	if !strings.HasPrefix(pk, "0x") {
		pk = "0x" + pk
	}
	if len(pk) != 66 {
		return nil, fmt.Errorf("invalid_private_key")
	}
	signer, err := auth.NewPrivateKeySigner(pk, cfg.ChainID)
	if err != nil {
		return nil, err
	}
	pc := polymarket.DefaultConfig()
	pc.BaseURLs.CLOB = cfg.PolymarketAPIURL
	opts := []polymarket.Option{polymarket.WithConfig(pc), polymarket.WithUseServerTime(true)}
	if cfg.HTTPPlatformProxy != "" {
		opts = append(opts, polymarket.WithProxyURL(cfg.HTTPPlatformProxy))
	}
	root := polymarket.NewClient(opts...)
	cl := root.CLOB.WithAuth(signer, nil)

	resp, err := cl.CreateOrDeriveAPIKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("CreateOrDeriveAPIKey: %w", err)
	}
	if strings.TrimSpace(resp.APIKey) == "" || strings.TrimSpace(resp.Secret) == "" || strings.TrimSpace(resp.Passphrase) == "" {
		return nil, fmt.Errorf("polymarket_api_key_incomplete")
	}
	funder, err := auth.DerivePolymarketDepositWallet(signer.Address())
	if err != nil {
		return nil, err
	}
	return &Creds{
		FunderAddress: strings.ToLower(funder.Hex()),
		APIKey:        resp.APIKey,
		Secret:        resp.Secret,
		Passphrase:    resp.Passphrase,
	}, nil
}

// ValidatePrivateKey parses hex key into address (lightweight check).
func ValidatePrivateKey(privateKeyHex string) (common.Address, error) {
	pk := strings.TrimSpace(privateKeyHex)
	if !strings.HasPrefix(pk, "0x") {
		pk = "0x" + pk
	}
	signer, err := auth.NewPrivateKeySigner(pk, 137)
	if err != nil {
		return common.Address{}, err
	}
	return signer.Address(), nil
}
