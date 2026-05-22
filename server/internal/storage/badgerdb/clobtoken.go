package badgerdb

import (
	"fmt"
	"math/big"
	"strings"
)

// CLOBTokenLookupVariants returns distinct token id strings for KeyMarketTokenLookup.
// Gamma sync stores decimal CLOB ids while risk positions use 0x-padded hex.
func CLOBTokenLookupVariants(tokenID string) []string {
	id := strings.TrimSpace(tokenID)
	if id == "" {
		return nil
	}
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 3)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(id)
	norm := NormalizeCLOBTokenID(id)
	add(norm)
	if strings.HasPrefix(strings.ToLower(norm), "0x") {
		if n, ok := new(big.Int).SetString(norm[2:], 16); ok && n.Sign() > 0 {
			add(n.String())
		}
	}
	return out
}

// NormalizeCLOBTokenID matches poly_ws token normalization: decimal uint256 → 0x+64 hex.
func NormalizeCLOBTokenID(tokenID string) string {
	id := strings.TrimSpace(tokenID)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(id), "0x") {
		id = strings.ToLower(id)
	} else {
		if n, ok := new(big.Int).SetString(id, 10); ok {
			id = "0x" + fmt.Sprintf("%064x", n)
		} else {
			id = "0x" + strings.ToLower(strings.TrimPrefix(id, "0x"))
		}
	}
	if len(id) < 66 && strings.HasPrefix(id, "0x") {
		id = "0x" + strings.Repeat("0", 66-len(id)) + id[2:]
	}
	return id
}
