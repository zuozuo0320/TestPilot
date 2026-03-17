// transaction.go — 事务管理器，统一封装数据库事务操作
package repository

import (
	"context"

	"gorm.io/gorm"
)

// TxManager 事务管理器
type TxManager struct {
	db *gorm.DB
}

// NewTxManager 创建事务管理器
func NewTxManager(db *gorm.DB) *TxManager {
	return &TxManager{db: db}
}

// WithTx 在事务中执行操作，自动提交或回滚
func (m *TxManager) WithTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// DB 获取底层数据库连接（用于非事务场景）
func (m *TxManager) DB() *gorm.DB {
	return m.db
}
