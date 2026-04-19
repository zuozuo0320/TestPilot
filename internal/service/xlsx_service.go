// xlsx_service.go — 用例 xlsx 导入/导出服务
package service

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// XlsxService 用例导入导出服务
type XlsxService struct {
	testCaseRepo repository.TestCaseRepository
	tagRepo      repository.TagRepository
}

// NewXlsxService 创建导入导出服务
func NewXlsxService(repo repository.TestCaseRepository, tagRepo repository.TagRepository) *XlsxService {
	return &XlsxService{testCaseRepo: repo, tagRepo: tagRepo}
}

// 导出列定义
var exportHeaders = []string{
	"ID", "用例名称", "用例等级", "所属模块", "评审结果", "执行结果",
	"标签", "前置条件", "步骤描述", "备注", "优先级",
}

// ExportToXlsx 导出项目用例为 xlsx，写入 writer
func (s *XlsxService) ExportToXlsx(ctx context.Context, projectID uint, w io.Writer) error {
	// 查询所有用例（大分页）
	items, _, err := s.testCaseRepo.ListPaged(ctx, projectID, repository.TestCaseFilter{
		Page:     1,
		PageSize: 100000,
	})
	if err != nil {
		return fmt.Errorf("query test cases: %w", err)
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "测试用例"
	index, _ := f.NewSheet(sheet)
	f.SetActiveSheet(index)
	_ = f.DeleteSheet("Sheet1")

	// 写表头
	for i, h := range exportHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	// 表头样式
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"5B21B6"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	_ = f.SetRowStyle(sheet, 1, 1, headerStyle)

	// 写数据行
	for row, item := range items {
		r := row + 2
		_ = f.SetCellValue(sheet, cellName(1, r), item.ID)
		_ = f.SetCellValue(sheet, cellName(2, r), item.Title)
		_ = f.SetCellValue(sheet, cellName(3, r), item.Level)
		_ = f.SetCellValue(sheet, cellName(4, r), item.ModulePath)
		_ = f.SetCellValue(sheet, cellName(5, r), item.ReviewResult)
		_ = f.SetCellValue(sheet, cellName(6, r), item.ExecResult)
		_ = f.SetCellValue(sheet, cellName(7, r), item.Tags)
		_ = f.SetCellValue(sheet, cellName(8, r), item.Precondition)
		_ = f.SetCellValue(sheet, cellName(9, r), item.Steps)
		_ = f.SetCellValue(sheet, cellName(10, r), item.Remark)
		_ = f.SetCellValue(sheet, cellName(11, r), item.Priority)
	}

	// 设置列宽
	widths := []float64{8, 40, 10, 20, 12, 12, 20, 30, 40, 30, 10}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, w)
	}

	return f.Write(w)
}

// 报表导出列定义
var reportHeaders = []string{
	"ID", "用例名称", "执行步骤", "优先级", "所属模块", "评审结果",
	"状态", "标签", "更新人", "更新时间", "备注",
}

// ExportReportToXlsx 导出用例报表为 xlsx（按当前筛选条件导出全部数据）
func (s *XlsxService) ExportReportToXlsx(ctx context.Context, projectID uint, filter repository.TestCaseFilter) ([]byte, error) {
	// 导出全部命中数据，不受分页限制
	filter.Page = 1
	filter.PageSize = 100000
	items, _, err := s.testCaseRepo.ListPaged(ctx, projectID, filter)
	if err != nil {
		return nil, fmt.Errorf("query test cases: %w", err)
	}
	// 批量填充 tag_list（仓库层不填充 gorm:"-" 字段）
	if len(items) > 0 && s.tagRepo != nil {
		ids := make([]uint, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		if tagMap, tagErr := s.tagRepo.ListByTestCaseIDs(ctx, ids); tagErr == nil {
			for i := range items {
				if tags, ok := tagMap[items[i].ID]; ok {
					items[i].TagList = tags
				}
			}
		}
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "用例报表"
	index, _ := f.NewSheet(sheet)
	f.SetActiveSheet(index)
	_ = f.DeleteSheet("Sheet1")

	// 写表头
	for i, h := range reportHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	// 表头样式
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"5B21B6"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	_ = f.SetRowStyle(sheet, 1, 1, headerStyle)

	// 状态映射
	statusMap := map[string]string{
		"draft": "草稿", "pending": "待评审", "active": "已生效", "discarded": "已废弃",
	}

	// 写数据行
	for row, item := range items {
		r := row + 2
		// 标签：tag_list 中的 name 用中文逗号拼接
		tagNames := make([]string, 0)
		for _, t := range item.TagList {
			if t.Name != "" {
				tagNames = append(tagNames, t.Name)
			}
		}
		// 如果 TagList 为空，回退到旧的 Tags 字段
		tagStr := strings.Join(tagNames, "，")
		if tagStr == "" {
			tagStr = strings.ReplaceAll(item.Tags, ",", "，")
		}

		statusText := statusMap[item.Status]
		if statusText == "" {
			statusText = item.Status
		}

		// 执行步骤格式化："1、步骤描述 | 预期结果"
		stepsFormatted := formatSteps(item.Steps)

		_ = f.SetCellValue(sheet, cellName(1, r), item.ID)
		_ = f.SetCellValue(sheet, cellName(2, r), item.Title)
		_ = f.SetCellValue(sheet, cellName(3, r), stepsFormatted)
		_ = f.SetCellValue(sheet, cellName(4, r), item.Level)
		_ = f.SetCellValue(sheet, cellName(5, r), item.ModulePath)
		_ = f.SetCellValue(sheet, cellName(6, r), item.ReviewResult)
		_ = f.SetCellValue(sheet, cellName(7, r), statusText)
		_ = f.SetCellValue(sheet, cellName(8, r), tagStr)
		_ = f.SetCellValue(sheet, cellName(9, r), item.UpdatedByName)
		_ = f.SetCellValue(sheet, cellName(10, r), item.UpdatedAt)
		_ = f.SetCellValue(sheet, cellName(11, r), item.Remark)
	}

	// 设置列宽
	widths := []float64{8, 40, 50, 10, 25, 12, 10, 20, 12, 20, 30}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, w)
	}

	// 写入 buffer 返回
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return buf.Bytes(), nil
}

// ImportFromXlsx 从 xlsx 文件导入用例
func (s *XlsxService) ImportFromXlsx(ctx context.Context, projectID, userID uint, reader io.Reader) (created int, skipped int, err error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return 0, 0, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	// 读取第一个 sheet
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return 0, 0, fmt.Errorf("no sheets in xlsx")
	}
	sheet := sheets[0]
	rows, err := f.GetRows(sheet)
	if err != nil {
		return 0, 0, fmt.Errorf("read rows: %w", err)
	}
	if len(rows) < 2 {
		return 0, 0, nil // 只有表头或为空
	}

	// 解析表头，找到列映射
	headerMap := map[string]int{}
	for i, h := range rows[0] {
		headerMap[strings.TrimSpace(h)] = i
	}

	getCol := func(row []string, name string) string {
		idx, ok := headerMap[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for _, row := range rows[1:] {
		title := getCol(row, "用例名称")
		if title == "" {
			skipped++
			continue
		}

		level := getCol(row, "用例等级")
		if level == "" {
			level = "P1"
		}
		modulePath := getCol(row, "所属模块")
		if modulePath == "" {
			modulePath = "/未分类"
		}
		reviewResult := getCol(row, "评审结果")
		if reviewResult == "" {
			reviewResult = "未评审"
		}
		execResult := getCol(row, "执行结果")
		if execResult == "" {
			execResult = "未执行"
		}
		priority := strings.ToLower(getCol(row, "优先级"))
		if priority == "" {
			priority = "medium"
		}

		tc := &model.TestCase{
			ProjectID:    projectID,
			Title:        title,
			Level:        level,
			ReviewResult: reviewResult,
			ExecResult:   execResult,
			ModulePath:   modulePath,
			Tags:         getCol(row, "标签"),
			Precondition: getCol(row, "前置条件"),
			Steps:        getCol(row, "步骤描述"),
			Remark:       getCol(row, "备注"),
			Priority:     priority,
			CreatedBy:    userID,
			UpdatedBy:    userID,
		}
		if err := s.testCaseRepo.Create(ctx, tc); err != nil {
			skipped++
			continue
		}
		created++
	}
	return created, skipped, nil
}

// formatSteps 将步骤文本格式化为带序号的格式："1、步骤描述 | 预期结果"
func formatSteps(raw string) string {
	lines := strings.Split(raw, "\n")
	var result []string
	idx := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		idx++
		result = append(result, fmt.Sprintf("%d、%s", idx, trimmed))
	}
	return strings.Join(result, "\n")
}

func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}
