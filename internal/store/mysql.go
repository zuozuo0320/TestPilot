package store

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"testpilot/internal/config"
)

func NewMySQL(cfg config.Config, logger *slog.Logger) (*gorm.DB, error) {
	var (
		db  *gorm.DB
		err error
	)

	for attempt := 1; attempt <= cfg.DBConnectRetries; attempt++ {
		db, err = gorm.Open(mysql.Open(cfg.MySQLDSN()), &gorm.Config{})
		if err == nil {
			sqlDB, dbErr := db.DB()
			if dbErr != nil {
				err = dbErr
			} else {
				sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
				sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
				sqlDB.SetConnMaxLifetime(time.Duration(cfg.DBConnMaxLifetime) * time.Minute)
				err = sqlDB.Ping()
			}
		}

		if err == nil {
			return db, nil
		}

		logger.Warn("mysql connection failed", "attempt", attempt, "error", err)
		time.Sleep(time.Duration(cfg.DBRetryDelaySecond) * time.Second)
	}

	return nil, fmt.Errorf("connect mysql failed after retries: %w", err)
}
