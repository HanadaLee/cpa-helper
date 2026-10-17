package app

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const billingWorkbookContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (a *App) exportUsageBilling(w http.ResponseWriter, r *http.Request, filters UsageFilters) error {
	filters = normalizedUsageFilters(filters)
	bill, err := a.buildUsageBilling(r.Context(), filters)
	if err != nil {
		return err
	}
	data, err := billingWorkbook(bill, filters, r.URL.Query().Get("lang") == "en", time.Now())
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("cost-accounting_%s_%s.xlsx",
		filters.Start.In(appTimeLocation).Format("20060102-150405"),
		filters.End.In(appTimeLocation).Format("20060102-150405"))
	w.Header().Set("Content-Type", billingWorkbookContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "private, no-store")
	_, err = w.Write(data.Bytes())
	return err
}

// Export the same fixed historical costs as the dashboard, with numeric cells
// (not compact display strings) and no pagination or model-price recalculation.
func billingWorkbook(bill usageBillingResponse, filters UsageFilters, english bool, exportedAt time.Time) (*bytes.Buffer, error) {
	text := func(zh, en string) string {
		if english {
			return en
		}
		return zh
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := text("费用核算", "Cost Accounting")
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	start, err := parseQueryTime(bill.Start)
	if err != nil {
		return nil, err
	}
	end, err := parseQueryTime(bill.End)
	if err != nil {
		return nil, err
	}
	filterLabels := []string{}
	if filters.UserID != nil {
		for _, user := range bill.Users {
			if user.UserID != nil && *user.UserID == *filters.UserID {
				filterLabels = append(filterLabels, text("用户", "User")+": "+user.Label+" ("+user.Username+")")
				break
			}
		}
	}
	for _, field := range []struct {
		label string
		value *string
	}{
		{text("服务商", "Provider"), filters.Provider}, {text("模型", "Model"), filters.Model},
		{text("接口", "Endpoint"), filters.Endpoint}, {text("KEY 描述", "Key description"), filters.APIKeyDescription},
		{text("来源", "Source"), filters.SourceKey}, {text("请求 ID", "Request ID"), filters.RequestID},
	} {
		if field.value != nil {
			filterLabels = append(filterLabels, field.label+": "+*field.value)
		}
	}
	if filters.Failed != nil {
		result := text("成功", "Success")
		if *filters.Failed {
			result = text("失败", "Failed")
		}
		filterLabels = append(filterLabels, text("结果", "Result")+": "+result)
	}
	filterText := strings.Join(filterLabels, " · ")
	if filterText == "" {
		filterText = text("全部", "All")
	}
	dateTime := func(value time.Time) string { return value.In(appTimeLocation).Format("2006-01-02 15:04:05") }
	rows := [][]any{
		{sheet},
		{text("统计期间（北京时间）", "Period (Beijing time)"), dateTime(start) + " — " + dateTime(end) + text("（结束时间不含）", " (end exclusive)")},
		{text("筛选条件", "Filters"), filterText},
		{text("导出时间（北京时间）", "Exported (Beijing time)"), dateTime(exportedAt)},
		{text("费用合计 USD", "Total cost USD"), bill.Summary.CostUSD, text("请求数", "Requests"), bill.Summary.Records, text("总 Token", "Total tokens"), bill.Summary.TotalTokens, text("用户数", "Users"), len(bill.Items)},
		{text("费用使用入库时固化金额；未定价请求不计入费用合计。", "Costs are fixed at ingestion; unpriced requests are excluded from the total.")},
		{},
		{text("用户昵称", "User nickname"), text("用户名", "Username"), text("请求数", "Requests"), text("失败数", "Failed requests"), text("总 Token", "Total tokens"), text("费用 USD", "Cost USD"), text("费用占比", "Cost share"), text("未定价请求", "Unpriced requests")},
	}
	for _, item := range bill.Items {
		label := item.Label
		if label == "" {
			label = text("未绑定用户", "Unattributed user")
		}
		share := 0.0
		if bill.Summary.CostUSD > 0 {
			share = item.CostUSD / bill.Summary.CostUSD
		}
		// SetSheetRow writes strings as text, even if a nickname starts with '='.
		rows = append(rows, []any{label, item.Username, item.Records, item.FailedRecords, item.TotalTokens, item.CostUSD, share, item.UnpricedRecords})
	}
	totalShare := 0.0
	if bill.Summary.CostUSD > 0 {
		totalShare = 1
	}
	rows = append(rows, []any{text("合计", "Total"), "", bill.Summary.Records, bill.Summary.FailedRecords, bill.Summary.TotalTokens, bill.Summary.CostUSD, totalShare, bill.Summary.UnpricedRecords})
	for i, row := range rows {
		if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+1), &row); err != nil {
			return nil, err
		}
	}
	costFormat := "#,##0.00######"
	styles := []excelize.Style{
		{Font: &excelize.Font{Bold: true, Size: 18, Color: "166534"}},
		{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"166534"}, Pattern: 1}},
		{Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}},
		{NumFmt: 3},
		{CustomNumFmt: &costFormat},
		{NumFmt: 10},
	}
	styleIDs := make([]int, len(styles))
	for i := range styles {
		styleIDs[i], err = f.NewStyle(&styles[i])
		if err != nil {
			return nil, err
		}
	}
	last := len(rows)
	if err := errors.Join(
		f.MergeCell(sheet, "A1", "H1"), f.MergeCell(sheet, "B2", "H2"),
		f.MergeCell(sheet, "B3", "H3"), f.MergeCell(sheet, "B4", "H4"), f.MergeCell(sheet, "A6", "H6"),
		f.SetCellStyle(sheet, "A1", "H1", styleIDs[0]), f.SetRowHeight(sheet, 1, 32),
		f.SetCellStyle(sheet, "A2", "H4", styleIDs[2]), f.SetRowHeight(sheet, 2, 32), f.SetRowHeight(sheet, 3, 48), f.SetRowHeight(sheet, 4, 32),
		f.SetCellStyle(sheet, "A8", "H8", styleIDs[1]), f.SetRowHeight(sheet, 8, 28),
		f.SetCellStyle(sheet, "A9", fmt.Sprintf("B%d", last), styleIDs[2]),
		f.SetCellStyle(sheet, "C9", fmt.Sprintf("E%d", last), styleIDs[3]),
		f.SetCellStyle(sheet, "F9", fmt.Sprintf("F%d", last), styleIDs[4]),
		f.SetCellStyle(sheet, "G9", fmt.Sprintf("G%d", last), styleIDs[5]),
		f.SetCellStyle(sheet, "H9", fmt.Sprintf("H%d", last), styleIDs[3]),
		f.SetCellStyle(sheet, "B5", "B5", styleIDs[4]),
		f.SetColWidth(sheet, "A", "B", 30), f.SetColWidth(sheet, "C", "H", 20),
		f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 8, TopLeftCell: "A9", ActivePane: "bottomLeft"}),
		f.AutoFilter(sheet, fmt.Sprintf("A8:H%d", 8+len(bill.Items)), nil),
	); err != nil {
		return nil, err
	}
	return f.WriteToBuffer()
}
