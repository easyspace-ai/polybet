package polysession

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/easyspace-ai/polysdk/pkg/auth"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/polyprov"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/store"
)

var (
	envMu       sync.Mutex
	envCached   *polywiring.AuthedCLOB
	envCacheErr error
)

// ResolveAuthedCLOB returns an authenticated CLOB (active DB account, else sole DB account,
// else env L2 triple + key, else provision from POLYMARKET_PRIVATE_KEY).
func ResolveAuthedCLOB(ctx context.Context, cfg *config.Config, st *store.Store) (*polywiring.AuthedCLOB, error) {
	acct, err := st.GetActivePolymarketAccount(ctx)
	if err != nil {
		return nil, err
	}
	if acct != nil {
		return polywiring.BuildAuthedCLOB(cfg, acct)
	}
	// Exactly one DB row: use it even when is_active=0, so API key / pk / funder stay consistent
	// (avoids falling through to .env keys that belong to another wallet).
	if solo, err := st.GetSingletonPolymarketAccount(ctx); err != nil {
		return nil, err
	} else if solo != nil {
		return polywiring.BuildAuthedCLOB(cfg, solo)
	}
	if cfg.PolyAPIKey != "" && cfg.PolyAPISecret != "" && cfg.PolyAPIPassphrase != "" && cfg.PolyPrivateKey != "" {
		funder := strings.TrimSpace(cfg.PolyFunderAddress)
		if funder == "" {
			signer, err := auth.NewPrivateKeySigner(normalizePK(cfg.PolyPrivateKey), cfg.ChainID)
			if err != nil {
				return nil, err
			}
			fw, err := auth.DerivePolymarketDepositWallet(signer.Address())
			if err != nil {
				return nil, err
			}
			funder = fw.Hex()
		}
		a := &store.PolymarketAccount{
			Name:          "env",
			APIKey:        cfg.PolyAPIKey,
			Secret:        cfg.PolyAPISecret,
			Passphrase:    cfg.PolyAPIPassphrase,
			PrivateKey:    normalizePK(cfg.PolyPrivateKey),
			FunderAddress: strings.ToLower(funder),
		}
		return polywiring.BuildAuthedCLOB(cfg, a)
	}
	if cfg.PolyPrivateKey == "" {
		return nil, fmt.Errorf("polymarket_not_configured")
	}
	envMu.Lock()
	defer envMu.Unlock()
	if envCached != nil {
		return envCached, nil
	}
	if envCacheErr != nil {
		return nil, envCacheErr
	}
	creds, err := polyprov.FromPrivateKey(ctx, cfg, cfg.PolyPrivateKey)
	if err != nil {
		envCacheErr = err
		return nil, err
	}
	a := &store.PolymarketAccount{
		Name:          "env_provisioned",
		APIKey:        creds.APIKey,
		Secret:        creds.Secret,
		Passphrase:    creds.Passphrase,
		PrivateKey:    normalizePK(cfg.PolyPrivateKey),
		FunderAddress: creds.FunderAddress,
	}
	cl, err := polywiring.BuildAuthedCLOB(cfg, a)
	if err != nil {
		envCacheErr = err
		return nil, err
	}
	envCached = cl
	return cl, nil
}

func normalizePK(pk string) string {
	pk = strings.TrimSpace(pk)
	if pk != "" && !strings.HasPrefix(pk, "0x") {
		pk = "0x" + pk
	}
	return pk
}

func InvalidateEnvCache() {
	envMu.Lock()
	defer envMu.Unlock()
	envCached = nil
	envCacheErr = nil
}
