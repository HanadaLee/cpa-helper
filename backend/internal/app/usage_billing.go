package app

import (
	"context"
	"net/http"
	"sort"
	"strings"
)

type usageBillingTotals struct {
	CostUSD         float64 `json:"cost_usd"`
	Records         int     `json:"records"`
	FailedRecords   int     `json:"failed_records"`
	TotalTokens     int     `json:"total_tokens"`
	UnpricedRecords int     `json:"unpriced_records"`
}

func (t *usageBillingTotals) add(m usageAggregateMetrics) {
	t.CostUSD = mathRound(t.CostUSD+m.cost, 8)
	t.Records += m.records
	t.FailedRecords += m.failed
	t.TotalTokens += m.tokens
	t.UnpricedRecords += m.unpriced
}

type usageBillingUser struct {
	UserID   *int   `json:"user_id"`
	Username string `json:"username"`
	Label    string `json:"label"`
}

type usageBillingItem struct {
	// Keep the HTTP DTO flat: the API's time-normalizing JSON writer does not
	// flatten anonymous embedded fields like encoding/json does.
	UserID          *int    `json:"user_id"`
	Username        string  `json:"username"`
	Label           string  `json:"label"`
	CostUSD         float64 `json:"cost_usd"`
	Records         int     `json:"records"`
	FailedRecords   int     `json:"failed_records"`
	TotalTokens     int     `json:"total_tokens"`
	UnpricedRecords int     `json:"unpriced_records"`
}

type usageBillingResponse struct {
	Start       string             `json:"start"`
	End         string             `json:"end"`
	Summary     usageBillingTotals `json:"summary"`
	ActiveUsers int                `json:"active_users"`
	Items       []usageBillingItem `json:"items"`
	Users       []usageBillingUser `json:"users"`
}

func (a *App) usageBilling(w http.ResponseWriter, r *http.Request, filters UsageFilters) error {
	response, err := a.buildUsageBilling(r.Context(), filters)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

// Billing uses the same fixed-cost, watermark-aware reader as usage analytics.
// It streams aggregates once for all users, not one query per user, and never
// consults current model prices or drops users with no requests in the range.
func (a *App) buildUsageBilling(ctx context.Context, filters UsageFilters) (usageBillingResponse, error) {
	filters = normalizedUsageFilters(filters)
	response := usageBillingResponse{
		Start: usageAPITime(*filters.Start), End: usageAPITime(*filters.End),
		Items: []usageBillingItem{}, Users: []usageBillingUser{},
	}
	if !filters.Start.Before(*filters.End) {
		return response, validationError("结束时间必须晚于开始时间")
	}
	users, err := a.userLookup(ctx, usageAccessScope{IsAdmin: true})
	if err != nil {
		return response, err
	}
	type billingGroup struct {
		usageBillingUser
		usageBillingTotals
	}
	groups := map[string]*billingGroup{}
	for username, info := range users {
		id := info.ID
		user := usageBillingUser{UserID: &id, Username: username, Label: info.Name}
		response.Users = append(response.Users, user)
		if filters.UserID == nil || *filters.UserID == id {
			groups[username] = &billingGroup{usageBillingUser: user}
			if filters.UserID != nil {
				name := username
				filters.UsageUsername = &name
			}
		}
	}
	// A missing user must not silently remove the scope and expose all users.
	if filters.UserID != nil && len(groups) == 0 {
		return response, notFoundError("用户不存在")
	}
	err = a.walkAggregateUsageEntries(ctx, filters, func(entry usageAggregateEntry) {
		username := strings.TrimSpace(valueOr(entry.record.UsageUsername, ""))
		item := groups[username]
		if item == nil {
			// Preserve orphaned/unattributed usage so item totals reconcile with
			// the platform total, including after a user is removed.
			item = &billingGroup{usageBillingUser: usageBillingUser{Username: username, Label: username}}
			groups[username] = item
		}
		item.add(entry.metrics)
		response.Summary.add(entry.metrics)
	})
	if err != nil {
		return response, err
	}
	for _, item := range groups {
		response.Items = append(response.Items, usageBillingItem{
			UserID: item.UserID, Username: item.Username, Label: item.Label,
			CostUSD: item.CostUSD, Records: item.Records, FailedRecords: item.FailedRecords,
			TotalTokens: item.TotalTokens, UnpricedRecords: item.UnpricedRecords,
		})
		if item.Records > 0 {
			response.ActiveUsers++
		}
	}
	sort.Slice(response.Items, func(i, j int) bool {
		a, b := response.Items[i], response.Items[j]
		if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		return a.Username < b.Username
	})
	sort.Slice(response.Users, func(i, j int) bool { return response.Users[i].Username < response.Users[j].Username })
	return response, nil
}
