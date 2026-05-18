package storage

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/accountsfile"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/store"
)

// InitBadger seeds config/bot from ~/.polybet/bot-settings.json when the key
// is missing. The Badger handle must already be opened on the backend.
func InitBadger(ctx context.Context, cfg *config.Config, b *Backend, log *logrus.Logger) error {
	if cfg == nil || b == nil || b.Badger == nil {
		return nil
	}
	db := b.Badger

	hasBot, err := db.HasConfigBot(ctx)
	if err != nil {
		return err
	}
	if !hasBot {
		if err := db.WriteBotConfigMap(ctx, store.BotConfigStringMap()); err != nil {
			return err
		}
	}

	if err := accountsfile.MigrateFromBadgerIfNeeded(ctx, db, log); err != nil {
		return err
	}

	if err := db.MigrateRiskPositionSeq(ctx); err != nil {
		return err
	}

	if log != nil {
		log.WithFields(logx.Pairs("dir", db.Dir(), "sync_writes", cfg.BadgerSyncWrites)).Info("BadgerDB persistence ready")
	}
	return nil
}

// CloseBadger closes the Badger handle (idempotent).
func CloseBadger(b *Backend) {
	if b == nil || b.Badger == nil {
		return
	}
	_ = b.Badger.Close()
	b.Badger = nil
	if b.Store != nil {
		b.Store.KV = nil
	}
}
