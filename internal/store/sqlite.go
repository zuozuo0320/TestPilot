package store

import (
	"fmt"
	"log/slog"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"testpilot/internal/config"
)

func NewSQLite(cfg config.Config, logger *slog.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(cfg.SQLitePath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite failed: %w", err)
	}
	logger.Info("sqlite connected", "path", cfg.SQLitePath)
	return db, nil
}
