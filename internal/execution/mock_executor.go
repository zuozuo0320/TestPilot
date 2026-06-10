package execution

import (
	crand "crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"

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
	status := "passed"
	if float64(secureRandomInt(10000))/10000 < m.failRate {
		status = "failed"
	}
	duration := 300 + secureRandomInt(2200)
	output := fmt.Sprintf("[mock-cypress] script=%s path=%s status=%s duration_ms=%d", script.Name, script.Path, status, duration)

	m.logger.Info("script executed", "script_id", script.ID, "script_name", script.Name, "status", status)

	return ScriptExecution{
		Status:     status,
		Output:     output,
		DurationMS: duration,
	}
}

func secureRandomInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(n.String())
	if err != nil {
		return 0
	}
	return value
}
