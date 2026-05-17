package badgerdb

import (
	"fmt"
	"math/big"
	"strings"
)

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
