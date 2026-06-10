// ai_script_asset_helpers.go — 测试智编资产服务共享辅助逻辑
package service

import (
	"context"
	"fmt"
	"log/slog"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func batchProjectNames(ctx context.Context, projectRepo repository.ProjectRepository, logger *slog.Logger, ids []uint) map[uint]string {
	result := make(map[uint]string, len(ids))
	projects, err := projectRepo.FindByIDs(ctx, ids)
	if err != nil {
		logger.Error("batch project names failed", "error", err)
		return result
	}
	for _, project := range projects {
		result[project.ID] = project.Name
	}
	return result
}

func batchUserNames(ctx context.Context, userRepo repository.UserRepository, logger *slog.Logger, ids []uint) map[uint]string {
	result := make(map[uint]string, len(ids))
	users, err := userRepo.FindByIDs(ctx, ids)
	if err != nil {
		logger.Error("batch user names failed", "error", err)
		return result
	}
	for _, user := range users {
		result[user.ID] = user.Name
	}
	return result
}

func optionalStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// expandAssetImpactReferences 展开直接和间接影响关系。
// 固定场景允许被其他固定场景引用，因此影响分析需要沿 FLOW 目标继续向上追踪。
func expandAssetImpactReferences(ctx context.Context, refRepo *repository.AIAssetReferenceRepo, targetType string, targetID uint, maxDepth int) ([]model.AIAssetReference, error) {
	if maxDepth < 1 {
		maxDepth = 1
	}
	result := make([]model.AIAssetReference, 0)
	seenRefs := make(map[string]struct{})
	visitedFlowTargets := map[uint]struct{}{}

	directRefs, err := refRepo.ListByTarget(ctx, targetType, targetID)
	if err != nil {
		return nil, err
	}
	nextFlowTargets := make([]uint, 0)
	for _, ref := range directRefs {
		ref.ImpactLevel = "DIRECT"
		ref.ImpactPath = []string{fmt.Sprintf("%s:%d", targetType, targetID), fmt.Sprintf("%s:%d", ref.SourceType, ref.SourceID)}
		result = appendUniqueImpactRef(result, seenRefs, ref)
		if ref.SourceType == model.AIAssetRefSourceFlow {
			nextFlowTargets = append(nextFlowTargets, ref.SourceID)
		}
	}

	for depth := 2; depth <= maxDepth && len(nextFlowTargets) > 0; depth++ {
		currentFlowTargets := deduplicateUints(nextFlowTargets)
		nextFlowTargets = nil
		queryFlowTargets := make([]uint, 0, len(currentFlowTargets))
		for _, flowID := range currentFlowTargets {
			if _, ok := visitedFlowTargets[flowID]; ok {
				continue
			}
			visitedFlowTargets[flowID] = struct{}{}
			queryFlowTargets = append(queryFlowTargets, flowID)
		}
		refs, err := refRepo.ListByTargets(ctx, model.AIAssetRefTargetFlow, queryFlowTargets)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			ref.ImpactLevel = "INDIRECT"
			ref.ImpactPath = []string{
				fmt.Sprintf("%s:%d", targetType, targetID),
				fmt.Sprintf("%s:%d", ref.TargetType, ref.TargetID),
				fmt.Sprintf("%s:%d", ref.SourceType, ref.SourceID),
			}
			result = appendUniqueImpactRef(result, seenRefs, ref)
			if ref.SourceType == model.AIAssetRefSourceFlow {
				nextFlowTargets = append(nextFlowTargets, ref.SourceID)
			}
		}
	}

	return result, nil
}

func appendUniqueImpactRef(result []model.AIAssetReference, seen map[string]struct{}, ref model.AIAssetReference) []model.AIAssetReference {
	key := fmt.Sprintf("%s:%d>%s:%d:%d", ref.SourceType, ref.SourceID, ref.TargetType, ref.TargetID, dereferenceUint(ref.TargetVersionID))
	if _, ok := seen[key]; ok {
		return result
	}
	seen[key] = struct{}{}
	return append(result, ref)
}

func dereferenceUint(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}
