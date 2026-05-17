// ai_model_config_handler.go — AI 模型配置 HTTP 处理器（仅 admin）
package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/service"
)

// listAIModelConfigs 列出所有 AI 模型配置
func (a *API) listAIModelConfigs(c *gin.Context) {
	configs, err := a.aiModelConfigSvc.List(c.Request.Context())
	if err != nil {
		response.HandleError(c, err)
		return
	}
	// 返回脱敏数据
	masked := make([]map[string]interface{}, len(configs))
	for i := range configs {
		masked[i] = configs[i].Masked()
	}
	response.OK(c, masked)
}

// getActiveAIModel 查询当前启用的模型
func (a *API) getActiveAIModel(c *gin.Context) {
	cfg, err := a.aiModelConfigSvc.GetActive(c.Request.Context())
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, cfg.Masked())
}

// createAIModelConfig 创建模型配置
func (a *API) createAIModelConfig(c *gin.Context) {
	var input service.CreateAIModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, 400, service.CodeParamsError, "参数错误: "+err.Error())
		return
	}
	cfg, err := a.aiModelConfigSvc.Create(c.Request.Context(), input)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, cfg.Masked())
}

// updateAIModelConfig 更新模型配置
func (a *API) updateAIModelConfig(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("configID"), 10, 64)
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "无效的配置 ID")
		return
	}
	var input service.UpdateAIModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, 400, service.CodeParamsError, "参数错误: "+err.Error())
		return
	}
	cfg, err := a.aiModelConfigSvc.Update(c.Request.Context(), uint(id), input)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, cfg.Masked())
}

// deleteAIModelConfig 删除模型配置
func (a *API) deleteAIModelConfig(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("configID"), 10, 64)
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "无效的配置 ID")
		return
	}
	if err := a.aiModelConfigSvc.Delete(c.Request.Context(), uint(id)); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, nil)
}

// testAIModelConnection 测试模型连接（不保存，仅验证 API Key 和 Base URL）
// 编辑已有配置时，api_key 可留空，由 config_id 从数据库读取
func (a *API) testAIModelConnection(c *gin.Context) {
	var input struct {
		ConfigID uint   `json:"config_id"`
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
		ModelID  string `json:"model_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, 400, service.CodeParamsError, "参数错误: "+err.Error())
		return
	}
	// 如果未传 api_key 但有 config_id，从数据库获取已存的 key
	apiKey := input.APIKey
	provider := input.Provider
	baseURL := input.BaseURL
	if apiKey == "" && input.ConfigID > 0 {
		existing, err := a.aiModelConfigSvc.GetByID(c.Request.Context(), input.ConfigID)
		if err != nil {
			response.Error(c, 400, service.CodeParamsError, "找不到已有配置")
			return
		}
		apiKey = existing.APIKey
		if provider == "" {
			provider = existing.Provider
		}
		if baseURL == "" {
			baseURL = existing.BaseURL
		}
	}
	if apiKey == "" {
		response.Error(c, 400, service.CodeParamsError, "请输入 API Key")
		return
	}
	result, err := a.aiModelConfigSvc.TestConnection(c.Request.Context(), provider, apiKey, baseURL, input.ModelID)
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, err.Error())
		return
	}
	response.OK(c, result)
}

// listAIModelOptions 拉取上游模型列表
func (a *API) listAIModelOptions(c *gin.Context) {
	var input struct {
		ConfigID uint   `json:"config_id"`
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, 400, service.CodeParamsError, "参数错误: "+err.Error())
		return
	}
	apiKey := input.APIKey
	provider := input.Provider
	baseURL := input.BaseURL
	if apiKey == "" && input.ConfigID > 0 {
		existing, err := a.aiModelConfigSvc.GetByID(c.Request.Context(), input.ConfigID)
		if err != nil {
			response.Error(c, 400, service.CodeParamsError, "找不到已有配置")
			return
		}
		apiKey = existing.APIKey
		if provider == "" {
			provider = existing.Provider
		}
		if baseURL == "" {
			baseURL = existing.BaseURL
		}
	}
	if apiKey == "" {
		response.Error(c, 400, service.CodeParamsError, "请输入 API Key")
		return
	}
	models, err := a.aiModelConfigSvc.ListModels(c.Request.Context(), service.ListAIModelsInput{
		Provider: provider,
		APIKey:   apiKey,
		BaseURL:  baseURL,
	})
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, err.Error())
		return
	}
	response.OK(c, models)
}

// activateAIModel 启用指定模型
func (a *API) activateAIModel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("configID"), 10, 64)
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "无效的配置 ID")
		return
	}
	cfg, err := a.aiModelConfigSvc.Activate(c.Request.Context(), uint(id))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, cfg.Masked())
}
