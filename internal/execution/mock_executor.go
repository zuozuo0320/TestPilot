package execution

import (
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"testpilot/internal/model"
)

type ScriptExecution struct {
	Status     string
	Output     string
	DurationMS int
}

type MockExecutor struct {
	logger   *slog.Logger
	failRate float64
}

func NewMockExecutor(logger *slog.Logger, failRate float64) *MockExecutor {
	if failRate < 0 || failRate > 1 {
		failRate = 0.25
	}
	return &MockExecutor{logger: logger, failRate: failRate}
}

func (m *MockExecutor) RunScript(script model.Script) ScriptExecution {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(script.ID)))
	status := "passed"
	if rng.Float64() < m.failRate {
		status = "failed"
	}
	duration := 300 + rng.Intn(2200)
	output := fmt.Sprintf("[mock-cypress] script=%s path=%s status=%s duration_ms=%d", script.Name, script.Path, status, duration)

	m.logger.Info("script executed", "script_id", script.ID, "script_name", script.Name, "status", status)

	return ScriptExecution{
		Status:     status,
		Output:     output,
		DurationMS: duration,
	}
}
