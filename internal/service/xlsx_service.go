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
}

// NewXlsxService 创建导入导出服务
func NewXlsxService(repo repository.TestCaseRepository) *XlsxService {
	return &XlsxService{testCaseRepo: repo}
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

func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}
