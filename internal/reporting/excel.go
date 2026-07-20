package reporting

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"

	"github.com/lzw7758/go-dot/internal/tracking"
)

const (
	detailSheet      = "数据明细"
	explanationSheet = "报表说明"
)

type ExcelExporter struct{}

func (ExcelExporter) Build(document ExportDocument) (contents []byte, returnErr error) {
	book := excelize.NewFile()
	defer func() {
		if err := book.Close(); err != nil {
			contents = nil
			returnErr = errors.Join(returnErr, err)
		}
	}()

	headerStyle, err := book.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"4472C4"}},
	})
	if err != nil {
		return nil, err
	}
	wrapStyle, err := book.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	if err != nil {
		return nil, err
	}
	if _, err = book.NewSheet(detailSheet); err != nil {
		return nil, err
	}
	if _, err = book.NewSheet(explanationSheet); err != nil {
		return nil, err
	}
	if err = writeDetailSheet(book, headerStyle, document.Result); err != nil {
		return nil, err
	}
	if err = writeExplanationSheet(book, headerStyle, wrapStyle, document); err != nil {
		return nil, err
	}
	if err = book.DeleteSheet("Sheet1"); err != nil {
		return nil, err
	}
	book.SetActiveSheet(0)

	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeDetailSheet(book *excelize.File, headerStyle int, result DailyResult) (returnErr error) {
	stream, err := book.NewStreamWriter(detailSheet)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, stream.Flush())
	}()

	if err = stream.SetColWidth(1, 1, 13); err != nil {
		return err
	}
	if err = stream.SetColWidth(2, 2, 11); err != nil {
		return err
	}
	if err = stream.SetColWidth(3, 4, 24); err != nil {
		return err
	}
	if err = stream.SetColWidth(5, 7, 12); err != nil {
		return err
	}
	if err = stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	header := []interface{}{
		excelize.Cell{StyleID: headerStyle, Value: "日期"},
		excelize.Cell{StyleID: headerStyle, Value: "数据状态"},
		excelize.Cell{StyleID: headerStyle, Value: "点位编码"},
		excelize.Cell{StyleID: headerStyle, Value: "点位名称"},
		excelize.Cell{StyleID: headerStyle, Value: "PV"},
		excelize.Cell{StyleID: headerStyle, Value: "UV"},
		excelize.Cell{StyleID: headerStyle, Value: "UV口径"},
	}
	if err = stream.SetRow("A1", header); err != nil {
		return err
	}
	for index, item := range result.Items {
		status := "未完成"
		if item.Complete {
			status = "完整"
		}
		uvMode := "近似"
		if item.UVMode == tracking.Exact {
			uvMode = "精确"
		}
		cell, coordinateErr := excelize.CoordinatesToCellName(1, index+2)
		if coordinateErr != nil {
			return coordinateErr
		}
		if err = stream.SetRow(cell, []interface{}{
			safeSpreadsheetText(item.Date), status, safeSpreadsheetText(item.PointCode),
			safeSpreadsheetText(item.PointName), item.PV, item.UV, uvMode,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeExplanationSheet(book *excelize.File, headerStyle, wrapStyle int, document ExportDocument) (returnErr error) {
	stream, err := book.NewStreamWriter(explanationSheet)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, stream.Flush())
	}()

	if err = stream.SetColWidth(1, 1, 16); err != nil {
		return err
	}
	if err = stream.SetColWidth(2, 2, 56); err != nil {
		return err
	}
	if err = stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	rows := [][]interface{}{
		{excelize.Cell{StyleID: headerStyle, Value: "项目"}, excelize.Cell{StyleID: headerStyle, Value: "内容"}},
		{"报表名称", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(document.ReportName)}},
		{"报表说明", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(document.ReportDescription)}},
		{"查询范围", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(document.Result.StartDate + " 至 " + document.Result.EndDate)}},
		{"统计时区", excelize.Cell{StyleID: wrapStyle, Value: "Asia/Shanghai"}},
		{"点位清单", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(pointList(document.Result.Items))}},
		{"导出时间", excelize.Cell{StyleID: wrapStyle, Value: document.ExportedAt.In(location).Format("2006-01-02 15:04:05")}},
		{"导出用户", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(document.ExportedBy)}},
	}
	for index, row := range rows {
		cell, coordinateErr := excelize.CoordinatesToCellName(1, index+1)
		if coordinateErr != nil {
			return coordinateErr
		}
		if err = stream.SetRow(cell, row); err != nil {
			return err
		}
	}
	return nil
}

func pointList(items []DailyItem) string {
	seen := make(map[uint64]struct{}, len(items))
	points := make([]string, 0, len(items))
	for _, item := range items {
		if _, exists := seen[item.PointID]; exists {
			continue
		}
		seen[item.PointID] = struct{}{}
		label := item.PointCode
		if item.PointName != "" {
			label = fmt.Sprintf("%s（%s）", item.PointCode, item.PointName)
		}
		points = append(points, label)
	}
	return strings.Join(points, "、")
}

func safeSpreadsheetText(value string) string {
	for _, current := range value {
		if unicode.IsSpace(current) || unicode.IsControl(current) || current == '\ufeff' {
			continue
		}
		switch current {
		case '=', '+', '-', '@':
			return "'" + value
		}
		break
	}
	return value
}
