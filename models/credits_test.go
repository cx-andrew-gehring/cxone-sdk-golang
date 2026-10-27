package models_test

import (
	"encoding/json"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// The informational actionsPerformed object must never break the decode of the
// surrounding item, whatever shape it takes.
func TestCreditConsumptionItemToleratesActionsPerformedShapes(t *testing.T) {
	cases := map[string]string{
		"null":         `null`,
		"empty":        `{}`,
		"old map":      `{"triage":1,"haiku-4.5":2}`,
		"string total": `{"actions":[{"actionType":"triage","transactionCount":4}],"total":"four"}`,
		"string":       `"x"`,
		"array":        `[1,2]`,
		"bad action":   `{"actions":[{"actionType":7,"transactionCount":{}}],"total":4}`,
		"actions obj":  `{"actions":{"a":1},"total":4}`,
	}
	for name, ap := range cases {
		t.Run(name, func(t *testing.T) {
			var it models.CreditConsumptionItem
			body := `{"name":"user-a","creditsUsed":"4","percentOfTotal":1.5,"actionsPerformed":` + ap + `}`
			if err := json.Unmarshal([]byte(body), &it); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if it.Name != "user-a" || it.CreditsUsed != "4" || it.PercentOfTotal != 1.5 {
				t.Errorf("item lost fields: %+v", it)
			}
		})
	}
}

func TestCreditActionsPerformedZeroOnMismatch(t *testing.T) {
	var a models.CreditActionsPerformed
	if err := json.Unmarshal([]byte(`{"triage":1}`), &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Actions) != 0 || a.Total != "" {
		t.Errorf("want zero value, got %+v", a)
	}
	var b models.CreditActionsPerformed
	if err := json.Unmarshal([]byte(`{"actions":[{"actionType":"triage","transactionCount":4}],"total":"x"}`), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Actions) != 1 || b.Actions[0].ActionType != "triage" || b.Total != "" {
		t.Errorf("want actions kept and total dropped, got %+v", b)
	}
}
