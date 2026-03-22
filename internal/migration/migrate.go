package migration

import (
	"embed"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed sql/*.sql
var migrationFS embed.FS

// SchemaMigration 迁移记录表
type SchemaMigration struct {
	ID      uint   `gorm:"primaryKey"`
	Version string `gorm:"size:255;uniqueIndex;not null"`
}

// Run 执行所有未应用的增量 SQL 迁移。
// 它在 GORM AutoMigrate 之后调用，负责处理 AutoMigrate 无法完成的变更（如修改列类型）。
func Run(db *gorm.DB, logger *slog.Logger) error {
	// 1. 确保迁移记录表存在
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// 2. 读取所有已执行的迁移版本
	var applied []SchemaMigration
	if err := db.Find(&applied).Error; err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	appliedSet := make(map[string]bool, len(applied))
	for _, m := range applied {
		appliedSet[m.Version] = true
	}

	// 3. 读取迁移文件
	entries, err := migrationFS.ReadDir("sql")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	// 按文件名排序，确保按顺序执行
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// 4. 逐个执行未应用的迁移
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if !strings.EqualFold(ext, ".sql") {
			continue
		}

		version := strings.TrimSuffix(name, ext)
		if appliedSet[version] {
			logger.Debug("migration already applied, skipping", "version", version)
			continue
		}

		content, err := migrationFS.ReadFile("sql/" + name)
		if err != nil {
			return fmt.Errorf("read migration file %s: %w", name, err)
		}

		logger.Info("applying migration", "version", version)

		// 按分号拆分为多条 SQL 逐条执行
		stmts := splitSQL(string(content))
		for _, stmt := range stmts {
			if stmt == "" {
				continue
			}
			if err := db.Exec(stmt).Error; err != nil {
				return fmt.Errorf("execute migration %s: %w\nSQL: %s", version, err, stmt)
			}
		}

		// 记录已执行
		if err := db.Create(&SchemaMigration{Version: version}).Error; err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		logger.Info("migration applied successfully", "version", version)
	}

	return nil
}

// splitSQL 将含有多条 SQL 的字符串按分号拆分，过滤注释和空行。
func splitSQL(content string) []string {
	lines := strings.Split(content, "\n")
	var buf strings.Builder
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 跳过纯注释行
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		buf.WriteString(trimmed)
		buf.WriteString(" ")
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(strings.TrimSuffix(buf.String(), " "))
			stmt = strings.TrimSuffix(stmt, ";")
			stmt = strings.TrimSpace(stmt)
			if stmt != "" {
				result = append(result, stmt)
			}
			buf.Reset()
		}
	}

	// 处理最后一条没有分号结尾的语句
	remaining := strings.TrimSpace(buf.String())
	if remaining != "" {
		result = append(result, remaining)
	}
	return result
}
