// ai_model_config_service.go — AI 模型配置业务层
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// AIModelConfigService AI 模型配置服务
type AIModelConfigService struct {
	repo           *repository.AIModelConfigRepo
	txMgr          *repository.TxManager
	executorURL    string // executor 内部地址（如 http://host.docker.internal:8100）
	executorAPIKey string // executor API Key
	logger         *slog.Logger
}

// NewAIModelConfigService 构造函数
func NewAIModelConfigService(
	repo *repository.AIModelConfigRepo,
	txMgr *repository.TxManager,
	executorURL string,
	executorAPIKey string,
	logger *slog.Logger,
) *AIModelConfigService {
	return &AIModelConfigService{
		repo:           repo,
		txMgr:          txMgr,
		executorURL:    executorURL,
		executorAPIKey: executorAPIKey,
		logger:         logger.With("module", "ai_model_config"),
	}
}

// List 查询所有模型配置
func (s *AIModelConfigService) List(ctx context.Context) ([]model.AIModelConfig, error) {
	return s.repo.List(ctx)
}

// GetByID 根据 ID 查询
func (s *AIModelConfigService) GetByID(ctx context.Context, id uint) (*model.AIModelConfig, error) {
	cfg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound(CodeAIModelNotFound, "模型配置不存在")
		}
		return nil, err
	}
	return cfg, nil
}

// GetActive 查询当前启用的模型
func (s *AIModelConfigService) GetActive(ctx context.Context) (*model.AIModelConfig, error) {
	cfg, err := s.repo.GetActive(ctx)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound(CodeAIModelNotFound, "未配置启用的模型")
		}
		return nil, err
	}
	return cfg, nil
}

// CreateInput 创建模型配置入参
type CreateAIModelInput struct {
	Provider string `json:"provider" binding:"required"`
	Name     string `json:"name" binding:"required"`
	ModelID  string `json:"model_id" binding:"required"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key" binding:"required"`
}

// Create 创建模型配置
func (s *AIModelConfigService) Create(ctx context.Context, input CreateAIModelInput) (*model.AIModelConfig, error) {
	cfg := &model.AIModelConfig{
		Provider: input.Provider,
		Name:     input.Name,
		ModelID:  input.ModelID,
		BaseURL:  input.BaseURL,
		APIKey:   input.APIKey,
	}
	if err := s.repo.Create(ctx, cfg, nil); err != nil {
		return nil, fmt.Errorf("创建模型配置失败: %w", err)
	}
	s.logger.Info("模型配置已创建", "id", cfg.ID, "model_id", cfg.ModelID)
	return cfg, nil
}

// UpdateAIModelInput 更新模型配置入参
type UpdateAIModelInput struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	ModelID  string `json:"model_id"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

// Update 更新模型配置
func (s *AIModelConfigService) Update(ctx context.Context, id uint, input UpdateAIModelInput) (*model.AIModelConfig, error) {
	cfg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound(CodeAIModelNotFound, "模型配置不存在")
		}
		return nil, err
	}
	if input.Provider != "" {
		cfg.Provider = input.Provider
	}
	if input.Name != "" {
		cfg.Name = input.Name
	}
	if input.ModelID != "" {
		cfg.ModelID = input.ModelID
	}
	if input.BaseURL != "" {
		cfg.BaseURL = input.BaseURL
	}
	if input.APIKey != "" {
		cfg.APIKey = input.APIKey
	}
	if err := s.repo.Update(ctx, cfg, nil); err != nil {
		return nil, fmt.Errorf("更新模型配置失败: %w", err)
	}
	s.logger.Info("模型配置已更新", "id", id, "model_id", cfg.ModelID)
	return cfg, nil
}

// Delete 删除模型配置
func (s *AIModelConfigService) Delete(ctx context.Context, id uint) error {
	cfg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrNotFound(CodeAIModelNotFound, "模型配置不存在")
		}
		return err
	}
	if cfg.IsActive {
		return ErrConflict(CodeAIModelIsActive, "不能删除当前启用的模型")
	}
	if err := s.repo.Delete(ctx, id, nil); err != nil {
		return fmt.Errorf("删除模型配置失败: %w", err)
	}
	s.logger.Info("模型配置已删除", "id", id)
	return nil
}

// Activate 启用指定模型（同时停用其他模型），并同步到 executor .env
func (s *AIModelConfigService) Activate(ctx context.Context, id uint) (*model.AIModelConfig, error) {
	var activated *model.AIModelConfig
	err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		cfg, err := s.repo.GetByID(ctx, id)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound(CodeAIModelNotFound, "模型配置不存在")
			}
			return err
		}
		// 先清除所有启用状态
		if err := s.repo.ClearActive(ctx, tx); err != nil {
			return fmt.Errorf("清除启用状态失败: %w", err)
		}
		cfg.IsActive = true
		if err := s.repo.Update(ctx, cfg, tx); err != nil {
			return fmt.Errorf("启用模型失败: %w", err)
		}
		activated = cfg
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 同步到 executor
	if syncErr := s.syncToExecutor(activated); syncErr != nil {
		s.logger.Error("同步模型配置到 executor 失败", "error", syncErr)
		// 不阻塞启用操作，仅记录日志
	}
	s.logger.Info("模型已启用", "id", id, "model_id", activated.ModelID)
	return activated, nil
}

// TestConnection 通过 executor 测试 LLM API 连通性
func (s *AIModelConfigService) TestConnection(ctx context.Context, apiKey, baseURL, modelID string) (map[string]string, error) {
	if s.executorURL == "" {
		return nil, fmt.Errorf("executor URL 未配置")
	}
	payload := map[string]string{
		"api_key":  apiKey,
		"base_url": baseURL,
		"model":    modelID,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", s.executorURL+"/config/model/test", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 executor: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := result["message"]
		if msg == "" {
			msg = fmt.Sprintf("测试失败 (HTTP %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return result, nil
}

// syncToExecutor 将模型配置推送到 executor /config/model 端点
func (s *AIModelConfigService) syncToExecutor(cfg *model.AIModelConfig) error {
	if s.executorURL == "" {
		return fmt.Errorf("executor URL 未配置")
	}
	payload := map[string]string{
		"api_key":  cfg.APIKey,
		"base_url": cfg.BaseURL,
		"model":    cfg.ModelID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}
	req, err := http.NewRequest("POST", s.executorURL+"/config/model", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 executor 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("executor 返回 %d", resp.StatusCode)
	}
	s.logger.Info("模型配置已同步到 executor", "model_id", cfg.ModelID)
	return nil
}
