package badgerdb

import "strconv"

// Key prefixes follow docs/ADR-badgerdb-migration.md (use '/' as separator).

func KeyMetaSchemaVersion() []byte { return []byte("meta/schema_version") }

// KeyRiskPositionSeq stores the monotonic counter for risk position display IDs.
func KeyRiskPositionSeq() []byte { return []byte("meta/risk_position_seq") }

func KeyConfigBot() []byte { return []byte("config/bot") }

func KeyAccount(id string) []byte {
	return []byte("account/" + id)
}

func KeyAccountActive() []byte { return []byte("account/active") }

func KeyMarketEvent(id string) []byte {
	return []byte("market/event/" + id)
}

func KeyMarketMarket(id string) []byte {
	return []byte("market/market/" + id)
}

func KeyMarketOutcome(id string) []byte {
	return []byte("market/outcome/" + id)
}

func KeyMarketCanonical(id string) []byte {
	return []byte("market/canonical/" + id)
}

func KeyMarketCanonOutcome(canonID, outcomeID string) []byte {
	return []byte("market/canonOut/" + canonID + "/" + outcomeID)
}

func KeyMarketTokenLookup(externalID string) []byte {
	return []byte("market/tokenLookup/" + externalID)
}

func KeyRiskPosition(id string) []byte {
	return []byte("risk/position/" + id)
}

func KeyRiskOpen(accountID, tokenID, sideLabel string) []byte {
	return []byte("risk/open/" + accountID + "/" + tokenID + "/" + sideLabel)
}

func KeyRiskClosed(accountID string, closedAtNano int64, positionID string) []byte {
	return []byte("risk/closed/" + accountID + "/" + strconv.FormatInt(closedAtNano, 10) + "/" + positionID)
}

func KeyRiskTask(id string) []byte {
	return []byte("risk/task/" + id)
}

func KeyRiskTaskDue(nextRunNano int64, taskID string) []byte {
	return []byte("risk/task/due/" + strconv.FormatInt(nextRunNano, 10) + "/" + taskID)
}

func KeyRiskApplied(accountID, tradeID string) []byte {
	return []byte("risk/applied/" + accountID + "/" + tradeID)
}

func KeyRiskHidden(accountID, tokenID, sideLabel string) []byte {
	return []byte("risk/hidden/" + accountID + "/" + tokenID + "/" + sideLabel)
}

func KeyTrade(id string) []byte {
	return []byte("trade/record/" + id)
}

func KeyTradeByAccount(accountID string, createdNano int64, id string) []byte {
	return []byte("trade/byAccount/" + accountID + "/" + strconv.FormatInt(createdNano, 10) + "/" + id)
}

func KeyTradeQuality(accountID string, tsNano int64, id string) []byte {
	return []byte("trade/quality/" + accountID + "/" + strconv.FormatInt(tsNano, 10) + "/" + id)
}
