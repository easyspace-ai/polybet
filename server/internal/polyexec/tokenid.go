package polyexec

import (
	"fmt"
	"math/big"
	"strings"
)

// CLOBAssetIDForAPI converts a Polymarket outcome token id to the form the CLOB
// HTTP API expects: a decimal string of the uint256 asset id. Hex ids
// (0x-prefixed, 64 hex digits) are converted the same way as market WS
// subscription in app.poly_ws / stoplossengine.clobAssetIDDecimal.
// Non-empty values that are already decimal digits are returned unchanged.
func CLOBAssetIDForAPI(hexOrDec string) string {
	s := strings.TrimSpace(hexOrDec)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if n, ok := new(big.Int).SetString(strings.TrimPrefix(strings.ToLower(s), "0x"), 16); ok {
			return n.String()
		}
		return ""
	}
	if n, ok := new(big.Int).SetString(s, 10); ok {
		return n.String()
	}
	return s
}

// MustCLOBAssetIDForAPI is like CLOBAssetIDForAPI but returns an error if the
// input cannot be normalized to a non-empty API token id.
func MustCLOBAssetIDForAPI(hexOrDec string) (string, error) {
	out := CLOBAssetIDForAPI(hexOrDec)
	if out == "" {
		return "", fmt.Errorf("invalid clob token id")
	}
	return out, nil
}
