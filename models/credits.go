package models

import "encoding/json"

// CreditConsumptionItem is one row of GET /api/v1/credits/consumption.
//
// CreditsUsed is deliberately a decimal string (for example "1.5" or "20"):
// the SDK never converts it to a float, so callers can parse and sum it
// exactly (math/big, a decimal library). ActionsPerformed is an informational
// breakdown of the actions behind the credits; it never affects the decode of
// the other fields (see CreditActionsPerformed). Unknown fields are ignored.
type CreditConsumptionItem struct {
	Name             string                 `json:"name"`
	CreditsUsed      string                 `json:"creditsUsed"`
	PercentOfTotal   float64                `json:"percentOfTotal"`
	ActionsPerformed CreditActionsPerformed `json:"actionsPerformed"`
}

// CreditActionsPerformed is the per-item breakdown of actions, for example
// {"actions":[{"actionType":"triage","transactionCount":4}],"total":4}. All
// fields are optional.
//
// The sub-object is informational and the endpoint is undocumented, so its
// decoding is deliberately tolerant: a value of an unexpected shape (null, a
// string, an array, a map of numbers, a non-numeric total, a malformed action)
// is ignored and leaves the zero value for that part. It never returns an
// error, so it can never make the whole consumption page fail to decode or
// cost the caller the item's CreditsUsed.
type CreditActionsPerformed struct {
	Actions []CreditActionCount `json:"actions,omitempty"`
	Total   json.Number         `json:"total,omitempty"`
}

// CreditActionCount is the number of transactions of one action type.
type CreditActionCount struct {
	ActionType       string      `json:"actionType,omitempty"`
	TransactionCount json.Number `json:"transactionCount,omitempty"`
}

// UnmarshalJSON decodes tolerantly: each field is decoded on its own and a
// field that does not match is left at its zero value. It always returns nil.
func (a *CreditActionsPerformed) UnmarshalJSON(data []byte) error {
	*a = CreditActionsPerformed{}
	var raw struct {
		Actions json.RawMessage `json:"actions"`
		Total   json.RawMessage `json:"total"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil // not an object (null, string, array, ...): informational, ignore
	}
	var total json.Number
	if json.Unmarshal(raw.Total, &total) == nil {
		a.Total = total
	}
	var elems []json.RawMessage
	if json.Unmarshal(raw.Actions, &elems) == nil {
		for _, e := range elems {
			var c CreditActionCount
			if json.Unmarshal(e, &c) == nil && (c.ActionType != "" || c.TransactionCount != "") {
				a.Actions = append(a.Actions, c)
			}
		}
	}
	return nil
}

// UnmarshalJSON decodes tolerantly: a field of an unexpected type is left at
// its zero value. It always returns nil.
func (c *CreditActionCount) UnmarshalJSON(data []byte) error {
	*c = CreditActionCount{}
	var raw struct {
		ActionType       json.RawMessage `json:"actionType"`
		TransactionCount json.RawMessage `json:"transactionCount"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	var t string
	if json.Unmarshal(raw.ActionType, &t) == nil {
		c.ActionType = t
	}
	var n json.Number
	if json.Unmarshal(raw.TransactionCount, &n) == nil {
		c.TransactionCount = n
	}
	return nil
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
