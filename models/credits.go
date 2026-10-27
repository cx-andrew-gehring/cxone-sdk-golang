package models

import "encoding/json"

// CreditConsumptionItem is one row of GET /api/v1/credits/consumption.
//
// CreditsUsed is deliberately a decimal string (for example "1.5" or "20"):
// the SDK never converts it to a float, so callers can parse and sum it
// exactly (math/big, a decimal library). ActionsPerformed maps an action name
// to a count; json.Number keeps whatever numeric form the service sent.
// Unknown fields are ignored.
type CreditConsumptionItem struct {
	Name             string                 `json:"name"`
	CreditsUsed      string                 `json:"creditsUsed"`
	PercentOfTotal   float64                `json:"percentOfTotal"`
	ActionsPerformed map[string]json.Number `json:"actionsPerformed,omitempty"`
}

// CreditConsumptionPage is one page of GET /api/v1/credits/consumption.
type CreditConsumptionPage struct {
	Items       []CreditConsumptionItem `json:"items"`
	TotalItems  int                     `json:"totalItems"`
	TotalPages  int                     `json:"totalPages"`
	CurrentPage int                     `json:"currentPage"`
}

// CreditConsumptionQuery selects one page of the consumption report.
//
// The paging query parameter names of this (undocumented) endpoint are not
// confirmed; they default to "page" and "limit" and can be overridden with
// PageParam and LimitParam. The first page number defaults to FirstPage 1;
// the SDK verifies that the response's currentPage equals the requested page
// and fails otherwise, so a wrong base can never silently skip a page.
type CreditConsumptionQuery struct {
	// Page is the page number to request; 0 sends no page parameter. Used by
	// the single-page call only (the all-pages call walks pages itself).
	Page int
	// Limit is the page size; 0 sends no limit parameter (service default).
	Limit int
	// PageParam / LimitParam override the query parameter names
	// (defaults "page" / "limit").
	PageParam  string
	LimitParam string
	// FirstPage is the number of the first page for the all-pages call
	// (default 1).
	FirstPage int
}
