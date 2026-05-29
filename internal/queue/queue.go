// Package queue 基于 Asynq 实现需求智生模块的异步任务队列。
//
// 设计目标（方案 B：worker 池同步驱动）：
//   - 后端创建生成任务后入队（Client），不再 fire-and-forget；
//   - worker 池（Server）按受控并发 Concurrency 消费，同步驱动 Executor 执行；
//   - 借助 Asynq 获得削峰、重试、背压与横向扩展能力。
//
// 资源隔离：复用平台 Redis 实例，但使用独立 DB index，避免与缓存键冲突。
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

// ========== 任务类型 & 队列名 ==========

const (
	// TypeRequirementGenerate 需求智生-用例生成任务类型
	TypeRequirementGenerate = "requirement_gen:generate"

	// QueueRequirementGen 需求智生专用队列名
	QueueRequirementGen = "requirement_gen"
)

// RequirementGeneratePayload 生成任务载荷
type RequirementGeneratePayload struct {
	TaskID uint `json:"task_id"` // 生成任务 ID
}

// RedisConfig Asynq 使用的 Redis 连接配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// toAsynqOpt 转换为 asynq 的 Redis 连接选项
func (c RedisConfig) toAsynqOpt() asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     c.Addr,
		Password: c.Password,
		DB:       c.DB,
	}
}

// ========== Client（入队端） ==========

// ClientOptions 入队默认参数
type ClientOptions struct {
	MaxRetry  int           // 最大重试次数
	Timeout   time.Duration // 单任务执行超时（应略大于 Executor 同步超时）
	Retention time.Duration // 完成任务的保留时长（便于排障）
}

// Client 生成任务入队客户端，封装 asynq.Client。
type Client struct {
	client *asynq.Client
	opts   ClientOptions
	logger *slog.Logger
}

// NewClient 创建入队客户端。
func NewClient(redisCfg RedisConfig, opts ClientOptions, logger *slog.Logger) *Client {
	return &Client{
		client: asynq.NewClient(redisCfg.toAsynqOpt()),
		opts:   opts,
		logger: logger.With("module", "queue_client"),
	}
}

// EnqueueGenerate 将生成任务入队。
// 使用 task_id 作为 Asynq TaskID 实现幂等去重：同一任务重复入队会被忽略。
func (c *Client) EnqueueGenerate(ctx context.Context, taskID uint) error {
	payload, err := json.Marshal(RequirementGeneratePayload{TaskID: taskID})
	if err != nil {
		return fmt.Errorf("marshal generate payload: %w", err)
	}

	task := asynq.NewTask(TypeRequirementGenerate, payload)
	info, err := c.client.EnqueueContext(ctx, task,
		asynq.Queue(QueueRequirementGen),
		asynq.TaskID(fmt.Sprintf("reqgen:%d", taskID)),
		asynq.MaxRetry(c.opts.MaxRetry),
		asynq.Timeout(c.opts.Timeout),
		asynq.Retention(c.opts.Retention),
	)
	if err != nil {
		// 任务已在队列/处理中：视为成功，避免重复执行
		if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
			c.logger.Warn("生成任务已在队列中，忽略重复入队", "task_id", taskID)
			return nil
		}
		return fmt.Errorf("enqueue generate task: %w", err)
	}
	c.logger.Info("生成任务已入队", "task_id", taskID, "queue", info.Queue, "asynq_id", info.ID)
	return nil
}

// Close 关闭入队客户端。
func (c *Client) Close() error {
	return c.client.Close()
}

// ========== Server（消费端） ==========

// GenerateHandlerFunc worker 处理生成任务的业务回调。
type GenerateHandlerFunc func(ctx context.Context, taskID uint) error

// Server 生成任务消费服务，封装 asynq.Server。
type Server struct {
	server *asynq.Server
	logger *slog.Logger
}

// NewServer 创建消费服务。concurrency 控制同时执行的生成任务数（核心并发闸门）。
func NewServer(redisCfg RedisConfig, concurrency int, logger *slog.Logger) *Server {
	srv := asynq.NewServer(redisCfg.toAsynqOpt(), asynq.Config{
		Concurrency: concurrency,
		Queues:      map[string]int{QueueRequirementGen: 1},
		LogLevel:    asynq.WarnLevel,
		ErrorHandler: asynq.ErrorHandlerFunc(func(_ context.Context, task *asynq.Task, err error) {
			logger.Error("队列任务处理失败", "type", task.Type(), "error", err)
		}),
	})
	return &Server{server: srv, logger: logger.With("module", "queue_server")}
}

// Start 非阻塞启动 worker，注册生成任务处理器。
func (s *Server) Start(handler GenerateHandlerFunc) error {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeRequirementGenerate, func(ctx context.Context, t *asynq.Task) error {
		var payload RequirementGeneratePayload
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			// 载荷损坏不可恢复，跳过重试
			return fmt.Errorf("unmarshal generate payload: %v: %w", err, asynq.SkipRetry)
		}
		return handler(ctx, payload.TaskID)
	})
	return s.server.Start(mux)
}

// Shutdown 优雅关停 worker：停止拉取新任务，等待在途任务完成。
func (s *Server) Shutdown() {
	s.server.Shutdown()
}
