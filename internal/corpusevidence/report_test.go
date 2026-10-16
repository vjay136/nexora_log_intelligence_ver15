package corpusevidence

import (
	"encoding/json"
	"testing"
)

func TestEmbeddedFieldAccountingEvidence(t *testing.T) {
	var report struct {
		Verdict string `json:"verdict"`
		Keys    struct {
			Declared int `json:"declared_family_keys"`
			Covered  int `json:"covered_family_keys"`
			Unmapped int `json:"unmapped_family_keys"`
		} `json:"known_key_inventory"`
		Totals struct {
			Records               int `json:"records"`
			Accounted             int `json:"accounted_records"`
			ExactParity           int `json:"exact_parity_records"`
			ExpectedQuarantined   int `json:"expected_quarantined_records"`
			UnexpectedQuarantined int `json:"unexpected_quarantined_records"`
			ParsedRaw             int `json:"parsed_comparable_raw_fields"`
			ParsedJSON            int `json:"parsed_generated_json_fields"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(JSON(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Verdict != "pass" || report.Totals.Records != 728 || report.Totals.Accounted != 728 ||
		report.Totals.ExactParity != 723 || report.Totals.ExpectedQuarantined != 5 || report.Totals.UnexpectedQuarantined != 0 ||
		report.Totals.ParsedRaw != report.Totals.ParsedJSON || report.Keys.Declared != 278 || report.Keys.Covered != 278 || report.Keys.Unmapped != 0 {
		t.Fatalf("embedded corpus evidence is incomplete or failing: %+v", report)
	}
}
