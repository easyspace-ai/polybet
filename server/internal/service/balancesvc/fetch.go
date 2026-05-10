package balancesvc

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Polymarket UI collateral token (pUSD) on Polygon — same as Node `balance.ts`.
const polymarketPUSD = "0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB"

// ParseCollateralMicro parses CLOB collateral balance string (micro-USDC) to dollars.
func ParseCollateralMicro(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty balance")
	}
	if !strings.Contains(s, ".") && !strings.ContainsAny(s, "eE") {
		n := new(big.Int)
		if _, ok := n.SetString(s, 10); ok {
			f := new(big.Float).Quo(new(big.Float).SetInt(n), big.NewFloat(1e6))
			v, _ := f.Float64()
			return v, nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v != v {
		return 0, fmt.Errorf("invalid_clob_collateral_balance")
	}
	return v, nil
}

func fetchCLOBCollateralUSD(ctx context.Context, cl *polywiring.AuthedCLOB) (float64, error) {
	resp, err := cl.Client.BalanceAllowance(ctx, &clobtypes.BalanceAllowanceRequest{
		AssetType: clobtypes.AssetTypeCollateral,
	})
	if err != nil {
		return 0, err
	}
	return ParseCollateralMicro(resp.Balance)
}

func readPUSDOnChain(ctx context.Context, rpcURL, ownerHex string) (float64, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	token := common.HexToAddress(polymarketPUSD)
	owner := common.HexToAddress(ownerHex)
	// balanceOf(address) selector
	data := append(common.Hex2Bytes("70a08231"), common.LeftPadBytes(owner.Bytes(), 32)...)
	res, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	if len(res) < 32 {
		return 0, fmt.Errorf("short_balance_response")
	}
	n := new(big.Int).SetBytes(res)
	f := new(big.Float).Quo(new(big.Float).SetInt(n), big.NewFloat(1e6))
	v, _ := f.Float64()
	return v, nil
}

func accountBalanceUSD(ctx context.Context, cfg *config.Config, acct *store.PolymarketAccount) *float64 {
	cl, err := polywiring.BuildAuthedCLOB(cfg, acct)
	if err != nil {
		return nil
	}
	ctx2, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	usd, err := fetchCLOBCollateralUSD(ctx2, cl)
	if err == nil {
		return &usd
	}
	usd2, err2 := readPUSDOnChain(ctx2, cfg.PolygonRPCURL, acct.FunderAddress)
	if err2 != nil {
		return nil
	}
	return &usd2
}

// Summary matches dashboard `BalanceSummary` / Node `fetchBalances`.
type Summary struct {
	Polymarket         *float64
	PolymarketAccounts []AccountRow
}

type AccountRow struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	IsActive   bool     `json:"isActive"`
	Polymarket *float64 `json:"polymarket"`
}

// Fetch returns per-account CLOB collateral (fallback on-chain pUSD), then sets top-level
// `polymarket` from the active account or env funder when no DB accounts.
func Fetch(ctx context.Context, cfg *config.Config, st *store.Store) (*Summary, error) {
	accts, err := st.ListPolymarketAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountRow, 0, len(accts))
	var top *float64
	for _, a := range accts {
		row := AccountRow{ID: a.ID, Name: a.Name, IsActive: a.IsActive}
		row.Polymarket = accountBalanceUSD(ctx, cfg, &a)
		out = append(out, row)
		if a.IsActive && row.Polymarket != nil {
			v := *row.Polymarket
			top = &v
		}
	}
	if top == nil && strings.TrimSpace(cfg.PolyFunderAddress) != "" {
		ctx2, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		if v, err := readPUSDOnChain(ctx2, cfg.PolygonRPCURL, cfg.PolyFunderAddress); err == nil {
			top = &v
		}
	}
	return &Summary{Polymarket: top, PolymarketAccounts: out}, nil
}
