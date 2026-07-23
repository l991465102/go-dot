package reporting

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/lzw7758/go-dot/internal/tracking"
)

func TestExcelExporterBuildsTwoSheetWorkbook(t *testing.T) {
	document := ExportDocument{
		Result: DailyResult{
			Timezone: "Asia/Shanghai", StartDate: "2026-07-19", EndDate: "2026-07-20",
			PointCount: 2, DayCount: 2, TotalPV: 15,
			Items: []DailyItem{
				{Date: "2026-07-20", Complete: false, PointID: 3, PointCode: "checkout", PointName: "结算", PV: 10, UV: 7, UVMode: tracking.Exact},
				{Date: "2026-07-19", Complete: true, PointID: 2, PointCode: "view", PointName: "浏览", PV: 5, UV: 4, UVMode: tracking.Approximate},
			},
		},
		ReportName:        "日报",
		ReportDescription: "核心漏斗",
		ExportedBy:        "alice",
		ExportedAt:        time.Date(2026, 7, 20, 2, 51, 3, 0, time.UTC),
	}

	contents, err := (ExcelExporter{}).Build(document)
	require.NoError(t, err)
	require.NotEmpty(t, contents)

	book, err := excelize.OpenReader(bytes.NewReader(contents))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, book.Close()) })
	require.Equal(t, []string{"点位统计", "报表说明"}, book.GetSheetList())

	detailRows, err := book.GetRows("点位统计")
	require.NoError(t, err)
	require.Equal(t, []string{"时间", "checkout_pv", "checkout_uv", "view_pv", "view_uv"}, detailRows[0])
	require.Equal(t, []string{"2026-07-20", "10", "7", "0", "0"}, detailRows[1])
	require.Equal(t, []string{"2026-07-19", "0", "0", "5", "4"}, detailRows[2])

	explanationRows, err := book.GetRows("报表说明")
	require.NoError(t, err)
	require.Equal(t, []string{"项目", "内容"}, explanationRows[0])
	require.Contains(t, explanationRows, []string{"统计时区", "Asia/Shanghai"})
	require.Contains(t, explanationRows, []string{"导出用户", "alice"})
	require.Contains(t, explanationRows, []string{"导出时间", "2026-07-20 10:51:03"})
	styleID, err := book.GetCellStyle("报表说明", "B3")
	require.NoError(t, err)
	style, err := book.GetStyle(styleID)
	require.NoError(t, err)
	require.NotNil(t, style.Alignment)
	require.True(t, style.Alignment.WrapText)
}

func TestExcelExporterProtectsEveryUserControlledTextCellFromFormulaInjection(t *testing.T) {
	dangerous := `=HYPERLINK("https://bad")`
	document := ExportDocument{
		Result: DailyResult{
			Timezone: "Asia/Shanghai", StartDate: "2026-07-20", EndDate: "2026-07-20",
			Items: []DailyItem{{Date: "2026-07-20", Complete: true, PointCode: "+code", PointName: "-name", UVMode: tracking.Exact}},
		},
		ReportName: dangerous, ReportDescription: "@description", ExportedBy: "=user",
		ExportedAt: time.Date(2026, 7, 20, 2, 51, 3, 0, time.UTC),
	}

	contents, err := (ExcelExporter{}).Build(document)
	require.NoError(t, err)
	book, err := excelize.OpenReader(bytes.NewReader(contents))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, book.Close()) })

	require.Equal(t, "'"+dangerous, cellValue(t, book, "报表说明", "B2"))
	require.Equal(t, "'@description", cellValue(t, book, "报表说明", "B3"))
	require.Equal(t, "'=user", cellValue(t, book, "报表说明", "B10"))
	require.Equal(t, "'+code_pv", cellValue(t, book, "点位统计", "B1"))
	require.Equal(t, "'+code_uv", cellValue(t, book, "点位统计", "C1"))
	for _, target := range []struct{ sheet, cell string }{
		{sheet: "报表说明", cell: "B2"}, {sheet: "报表说明", cell: "B3"},
		{sheet: "报表说明", cell: "B10"}, {sheet: "点位统计", cell: "B1"},
		{sheet: "点位统计", cell: "C1"},
	} {
		formula, err := book.GetCellFormula(target.sheet, target.cell)
		require.NoError(t, err)
		require.Empty(t, formula)
	}
}

func TestExcelExporterAddsOnlySelectedPropertySheets(t *testing.T) {
	sum, avg := "12.5", "6.25"
	document := ExportDocument{
		Result: DailyResult{
			StartDate: "2026-07-19", EndDate: "2026-07-20", IncompleteDates: []string{"2026-07-20"},
			MetricSections: []MetricSection{{
				Columns: []MetricColumn{
					{Key: "checkout_amount_sum", Label: "checkout_amount_sum", PointCode: "checkout", PropertyCode: "amount", Aggregation: tracking.AggregateSum},
					{Key: "checkout_amount_avg", Label: "checkout_amount_avg", PointCode: "checkout", PropertyCode: "amount", Aggregation: tracking.AggregateAvg},
				},
				Rows: []MetricRow{{Date: "2026-07-20", Values: map[string]*string{"checkout_amount_sum": &sum, "checkout_amount_avg": &avg}}},
			}},
			DimensionSections: []DimensionSection{{
				PointID: 3, PointCode: "checkout", PropertyCode: "status", PropertyName: "状态",
				Rows: []DimensionRow{
					{Date: "2026-07-20", Value: "success", EventCount: 3, UV: 2, Share: 1},
					{Date: "2026-07-20", Value: "", EventCount: 1, UV: 1, Share: 0.25},
				},
			}},
		},
		ExportedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}

	contents, err := (ExcelExporter{}).Build(document)
	require.NoError(t, err)
	book, err := excelize.OpenReader(bytes.NewReader(contents))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, book.Close()) })
	require.Equal(t, []string{"点位统计", "数值属性", "分类-checkout_status", "报表说明"}, book.GetSheetList())
	metricRows, err := book.GetRows("数值属性")
	require.NoError(t, err)
	require.Equal(t, []string{"时间", "checkout_amount_sum", "checkout_amount_avg"}, metricRows[0])
	require.Equal(t, []string{"2026-07-20", "12.5", "6.25"}, metricRows[1])
	dimensionRows, err := book.GetRows("分类-checkout_status")
	require.NoError(t, err)
	require.Equal(t, []string{"2026-07-20", "success", "3", "2", "1"}, dimensionRows[1])
	require.Equal(t, "空字符串", dimensionRows[2][1])
	require.Equal(t, "checkout.amount[SUM]、checkout.amount[AVG]、checkout.status[分类]", cellValue(t, book, "报表说明", "B7"))
	require.Equal(t, "2026-07-20", cellValue(t, book, "报表说明", "B8"))
}

func TestSafeSpreadsheetTextDetectsOperatorsAfterIgnorablePrefix(t *testing.T) {
	for _, value := range []string{
		"\t=tab", "\r+carriage", "\n-line", "  @spaces", "\u00a0=value", "\ufeff+value",
		"\t\r\n \u00a0\ufeff@combined",
	} {
		t.Run(value, func(t *testing.T) {
			require.Equal(t, "'"+value, safeSpreadsheetText(value))

			contents, err := (ExcelExporter{}).Build(ExportDocument{
				Result:     DailyResult{StartDate: "2026-07-20", EndDate: "2026-07-20"},
				ReportName: value, ExportedAt: time.Date(2026, 7, 20, 2, 51, 3, 0, time.UTC),
			})
			require.NoError(t, err)
			book, err := excelize.OpenReader(bytes.NewReader(contents))
			require.NoError(t, err)
			formula, err := book.GetCellFormula("报表说明", "B2")
			require.NoError(t, err)
			require.Empty(t, formula)
			require.NoError(t, book.Close())
		})
	}
}

func TestMetricExcelValuePreservesFixedPointPrecision(t *testing.T) {
	value := "99999999999999999999999999999.123456789"
	require.Equal(t, value, metricExcelValue(tracking.AggregateSum, value))
}

func cellValue(t *testing.T, book *excelize.File, sheet, cell string) string {
	t.Helper()
	value, err := book.GetCellValue(sheet, cell)
	require.NoError(t, err)
	return value
}
