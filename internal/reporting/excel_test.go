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
	require.Equal(t, []string{"数据明细", "报表说明"}, book.GetSheetList())

	detailRows, err := book.GetRows("数据明细")
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
	require.Equal(t, "'=user", cellValue(t, book, "报表说明", "B8"))
	require.Equal(t, "'+code_pv", cellValue(t, book, "数据明细", "B1"))
	require.Equal(t, "'+code_uv", cellValue(t, book, "数据明细", "C1"))
	for _, target := range []struct{ sheet, cell string }{
		{sheet: "报表说明", cell: "B2"}, {sheet: "报表说明", cell: "B3"},
		{sheet: "报表说明", cell: "B8"}, {sheet: "数据明细", cell: "B1"},
		{sheet: "数据明细", cell: "C1"},
	} {
		formula, err := book.GetCellFormula(target.sheet, target.cell)
		require.NoError(t, err)
		require.Empty(t, formula)
	}
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

func cellValue(t *testing.T, book *excelize.File, sheet, cell string) string {
	t.Helper()
	value, err := book.GetCellValue(sheet, cell)
	require.NoError(t, err)
	return value
}
