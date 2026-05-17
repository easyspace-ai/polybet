package store

import "github.com/easyspace-ai/polybet/internal/storage/badgerdb"

// RiskHiddenPosition is one persisted hide row (debug / unhide tooling).
type RiskHiddenPosition = badgerdb.RiskHiddenRow

func riskHiddenCompositeKey(tokenID, sideLabel string) string {
	return badgerdb.NormalizeCLOBTokenID(tokenID) + "\x1f" + sideLabel
}

// RiskPositionMonitorKey is the composite key for hidden-from-monitoring rows (token + side).
func RiskPositionMonitorKey(tokenID, sideLabel string) string {
	return riskHiddenCompositeKey(tokenID, sideLabel)
}
