package reporting

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"

	"github.com/lzw7758/go-dot/internal/tracking"
)

const (
	detailSheet      = "点位统计"
	metricSheet      = "数值属性"
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
	if len(document.Result.MetricSections) > 0 {
		if _, err = book.NewSheet(metricSheet); err != nil {
			return nil, err
		}
	}
	dimensionSheets := make([]string, len(document.Result.DimensionSections))
	usedSheets := map[string]struct{}{detailSheet: {}, metricSheet: {}, explanationSheet: {}}
	for index, section := range document.Result.DimensionSections {
		name := uniqueSheetName("分类-"+section.PointCode+"_"+section.PropertyCode, usedSheets)
		dimensionSheets[index] = name
		if _, err = book.NewSheet(name); err != nil {
			return nil, err
		}
	}
	if _, err = book.NewSheet(explanationSheet); err != nil {
		return nil, err
	}
	if err = writeDetailSheet(book, headerStyle, document.Result); err != nil {
		return nil, err
	}
	if len(document.Result.MetricSections) > 0 {
		if err = writeMetricSheet(book, headerStyle, document.Result.MetricSections[0]); err != nil {
			return nil, err
		}
	}
	for index, section := range document.Result.DimensionSections {
		if err = writeDimensionSheet(book, headerStyle, dimensionSheets[index], section); err != nil {
			return nil, err
		}
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

func writeMetricSheet(book *excelize.File, headerStyle int, section MetricSection) (returnErr error) {
	stream, err := book.NewStreamWriter(metricSheet)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, stream.Flush()) }()
	if err = stream.SetColWidth(1, 1, 13); err != nil {
		return err
	}
	if len(section.Columns) > 0 {
		if err = stream.SetColWidth(2, 1+len(section.Columns), 20); err != nil {
			return err
		}
	}
	if err = stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	header := []interface{}{excelize.Cell{StyleID: headerStyle, Value: "时间"}}
	for _, column := range section.Columns {
		header = append(header, excelize.Cell{StyleID: headerStyle, Value: safeSpreadsheetText(column.Label)})
	}
	if err = stream.SetRow("A1", header); err != nil {
		return err
	}
	for index, item := range section.Rows {
		row := []interface{}{safeSpreadsheetText(item.Date)}
		for _, column := range section.Columns {
			value := item.Values[column.Key]
			if value == nil {
				row = append(row, nil)
			} else {
				row = append(row, metricExcelValue(column.Aggregation, *value))
			}
		}
		cell, coordinateErr := excelize.CoordinatesToCellName(1, index+2)
		if coordinateErr != nil {
			return coordinateErr
		}
		if err = stream.SetRow(cell, row); err != nil {
			return err
		}
	}
	return nil
}

func metricExcelValue(aggregation tracking.MetricAggregation, value string) any {
	if aggregation == tracking.AggregateCount {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return parsed
		}
	} else if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}
	return safeSpreadsheetText(value)
}

func writeDimensionSheet(book *excelize.File, headerStyle int, sheet string, section DimensionSection) (returnErr error) {
	stream, err := book.NewStreamWriter(sheet)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, stream.Flush()) }()
	if err = stream.SetColWidth(1, 5, 18); err != nil {
		return err
	}
	if err = stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	header := []interface{}{
		excelize.Cell{StyleID: headerStyle, Value: "时间"},
		excelize.Cell{StyleID: headerStyle, Value: "属性值"},
		excelize.Cell{StyleID: headerStyle, Value: "事件数"},
		excelize.Cell{StyleID: headerStyle, Value: "UV"},
		excelize.Cell{StyleID: headerStyle, Value: "占比"},
	}
	if err = stream.SetRow("A1", header); err != nil {
		return err
	}
	for index, item := range section.Rows {
		cell, coordinateErr := excelize.CoordinatesToCellName(1, index+2)
		if coordinateErr != nil {
			return coordinateErr
		}
		if err = stream.SetRow(cell, []interface{}{safeSpreadsheetText(item.Date), safeSpreadsheetText(dimensionDisplayValue(item.Value)), item.EventCount, item.UV, item.Share}); err != nil {
			return err
		}
	}
	return nil
}

func uniqueSheetName(raw string, used map[string]struct{}) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\\`, r) || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, raw)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "分类属性"
	}
	runes := []rune(name)
	if len(runes) > 31 {
		name = string(runes[:31])
	}
	base := name
	for suffix := 2; ; suffix++ {
		if _, exists := used[name]; !exists {
			used[name] = struct{}{}
			return name
		}
		tail := fmt.Sprintf("-%d", suffix)
		baseRunes := []rune(base)
		if len(baseRunes)+len([]rune(tail)) > 31 {
			baseRunes = baseRunes[:31-len([]rune(tail))]
		}
		name = string(baseRunes) + tail
	}
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
	if err = stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	points, dates, values := dailyWideLayout(result.Items)
	if len(points) > 0 {
		if err = stream.SetColWidth(2, 1+len(points)*2, 18); err != nil {
			return err
		}
	}
	header := make([]interface{}, 1, 1+len(points)*2)
	header[0] = excelize.Cell{StyleID: headerStyle, Value: "时间"}
	for _, point := range points {
		header = append(header,
			excelize.Cell{StyleID: headerStyle, Value: safeSpreadsheetText(point.Code + "_pv")},
			excelize.Cell{StyleID: headerStyle, Value: safeSpreadsheetText(point.Code + "_uv")},
		)
	}
	if err = stream.SetRow("A1", header); err != nil {
		return err
	}
	for index, date := range dates {
		cell, coordinateErr := excelize.CoordinatesToCellName(1, index+2)
		if coordinateErr != nil {
			return coordinateErr
		}
		row := make([]interface{}, 1, 1+len(points)*2)
		row[0] = safeSpreadsheetText(date)
		for _, point := range points {
			item := values[dailyWideKey{Date: date, PointID: point.ID}]
			row = append(row, item.PV, item.UV)
		}
		if err = stream.SetRow(cell, row); err != nil {
			return err
		}
	}
	return nil
}

type dailyWidePoint struct {
	ID   uint64
	Code string
}

type dailyWideKey struct {
	Date    string
	PointID uint64
}

func dailyWideLayout(items []DailyItem) ([]dailyWidePoint, []string, map[dailyWideKey]DailyItem) {
	points := make([]dailyWidePoint, 0)
	dates := make([]string, 0)
	values := make(map[dailyWideKey]DailyItem, len(items))
	seenPoints := make(map[uint64]struct{})
	seenDates := make(map[string]struct{})

	for _, item := range items {
		if _, exists := seenPoints[item.PointID]; !exists {
			seenPoints[item.PointID] = struct{}{}
			points = append(points, dailyWidePoint{ID: item.PointID, Code: item.PointCode})
		}
		if _, exists := seenDates[item.Date]; !exists {
			seenDates[item.Date] = struct{}{}
			dates = append(dates, item.Date)
		}
		values[dailyWideKey{Date: item.Date, PointID: item.PointID}] = item
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return points, dates, values
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
		{"属性选择", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(propertySelectionList(document.Result))}},
		{"未完整日期", excelize.Cell{StyleID: wrapStyle, Value: safeSpreadsheetText(strings.Join(document.Result.IncompleteDates, "、"))}},
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

func propertySelectionList(result DailyResult) string {
	selections := make([]string, 0)
	for _, section := range result.MetricSections {
		for _, column := range section.Columns {
			selections = append(selections, fmt.Sprintf("%s.%s[%s]", column.PointCode, column.PropertyCode, column.Aggregation))
		}
	}
	for _, section := range result.DimensionSections {
		selections = append(selections, fmt.Sprintf("%s.%s[分类]", section.PointCode, section.PropertyCode))
	}
	return strings.Join(selections, "、")
}

func dimensionDisplayValue(value string) string {
	if value == "" {
		return "空字符串"
	}
	return value
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
