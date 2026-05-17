package accountsfile

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

// MigrateFromBadgerIfNeeded copies Polymarket accounts from legacy Badger keys
// into the JSON file once, then clears Badger account keys. Skips if the file
// already has at least one account.
func MigrateFromBadgerIfNeeded(ctx context.Context, db *badgerdb.DB, log *logrus.Logger) error {
	if db == nil {
		return nil
	}
	af := Default()
	existing, err := af.ReadAccounts(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	list, err := db.ReadAccounts(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}
	activeID, err := db.ReadActiveAccountID(ctx)
	if err != nil {
		return err
	}
	if err := af.WriteSnapshot(ctx, list, activeID); err != nil {
		return err
	}
	if err := db.WriteAccountsSnapshot(ctx, nil, ""); err != nil {
		return err
	}
	if log != nil {
		log.WithFields(logx.Pairs("path", af.Path(), "count", len(list))).Info("Polymarket 账号已从 Badger 迁移到 JSON 文件")
	}
	return nil
}
