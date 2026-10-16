package parserpolicy

import (
	"context"
	"reflect"
	"testing"
)

func TestSemanticExtractionStageParsesAndPreservesEvidence(t *testing.T) {
	pack := productionTestPack([]Stage{
		{ID: "recognize", Name: "Recognize email headers", Type: StageTypeRegex, Config: map[string]any{"pattern": `(?s)^(?P<kind>Subject):.*$`}, FailurePolicy: FailurePolicyRejectRule},
		{ID: "entities", Name: "Extract network and mail fields", Type: StageTypeSemanticExtract, Config: map[string]any{"entities": []string{"ipv4", "ipv6", "domain", "url", "email"}, "decode_mime_headers": true}, FailurePolicy: FailurePolicyRejectRule},
	})
	pack.Rules[0].CountPolicy = CountPolicyAllowDerived
	pack.Rules[0].FailurePolicy = FailurePolicyRejectRule
	if err := SealParserPack(pack); err != nil {
		t.Fatal(err)
	}
	input := "Subject: =?UTF-8?Q?Hello?=\r\n\t=?UTF-8?B?Vjg=?=\r\nX-Trace: peer=192.0.2.8 host=mail.example.org url=https://user:pass@example.org/x?token=secret email=a@example.org\r\n\r\n"
	executor, err := NewParserPackExecutor(pack)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(&ExecutionRequest{Input: input, Service: "smtp", NodeID: "test", Context: context.Background()})
	if err != nil || result.Quarantined {
		t.Fatalf("execution: result=%+v err=%v", result, err)
	}
	fields := result.EventPayload.Fields
	if fields["email_subject_decoded"] == nil || fields["network_ipv4"] == nil || fields["network_domain"] == nil || fields["url"] == nil || fields["email_address"] == nil {
		t.Fatalf("missing semantic fields: %+v", fields)
	}
	if result.ParseTelemetry.SourceFieldCount != 1 || result.ParseTelemetry.OutputFieldCount <= result.ParseTelemetry.SourceFieldCount {
		t.Fatalf("source/derived count accounting failed: %+v", result.ParseTelemetry)
	}
	foundEvidence := false
	for _, item := range result.ParseTelemetry.FieldEvidence {
		if item.Field == "network_ipv4" && item.Start >= 0 && item.End > item.Start && item.DerivedRule == "semantic_extract:ipv4" {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("semantic field omitted source evidence: %+v", result.ParseTelemetry.FieldEvidence)
	}
}

func TestSemanticExtractionRequiresExplicitDerivedCountPolicy(t *testing.T) {
	pack := productionTestPack([]Stage{{ID: "entities", Name: "Extract", Type: StageTypeSemanticExtract, Config: map[string]any{"entities": []string{"ipv4"}}, FailurePolicy: FailurePolicyRejectRule}})
	if result := ValidateParserPack(pack); result.Valid {
		t.Fatal("lossless exact-source count policy must not accept additional derived fields")
	}
}

func TestSemanticExtractionCoversNetworkMailAndEncodedSubjectVariants(t *testing.T) {
	pack := productionTestPack([]Stage{
		{ID: "recognize", Name: "Recognize email headers", Type: StageTypeRegex, Config: map[string]any{"pattern": `(?s)^(?P<kind>Subject):.*$`}, FailurePolicy: FailurePolicyRejectRule},
		{ID: "entities", Name: "Extract network and mail fields", Type: StageTypeSemanticExtract, Config: map[string]any{"entities": []string{"ipv4", "ipv6", "domain", "url", "email"}, "decode_mime_headers": true}, FailurePolicy: FailurePolicyRejectRule},
	})
	pack.Rules[0].CountPolicy = CountPolicyAllowDerived
	pack.Rules[0].FailurePolicy = FailurePolicyRejectRule
	if err := SealParserPack(pack); err != nil {
		t.Fatal(err)
	}
	input := "Subject: =?UTF-8?Q?IPv6_=E2=9C=93?=\r\n" +
		"X-Trace: v4=192.0.2.8 v6=2001:db8::1 host=mail.example.org " +
		"url=https://user:password@[2001:db8::1]/events?token=secret " +
		"email=Ops.Team+tag@example.org\r\n\r\n"
	executor, err := NewParserPackExecutor(pack)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(&ExecutionRequest{Input: input, Service: "smtp", NodeID: "test", Context: context.Background()})
	if err != nil || result.Quarantined {
		t.Fatalf("execution: result=%+v err=%v", result, err)
	}
	fields := result.EventPayload.Fields
	assertSemanticValues(t, fields, "network_ipv4", []interface{}{"192.0.2.8"})
	assertSemanticValues(t, fields, "network_ipv6", []interface{}{"2001:db8::1"})
	assertSemanticValues(t, fields, "email_address", []interface{}{"Ops.Team+tag@example.org"})
	assertSemanticValues(t, fields, "email_subject_decoded", []interface{}{"IPv6 ✓"})
	assertSemanticValues(t, fields, "url", []interface{}{"https://[2001:db8::1]/events?token=%5BREDACTED%5D"})
	if result.ParseTelemetry.InputFieldCount != result.ParseTelemetry.SourceFieldCount ||
		result.ParseTelemetry.OutputFieldCount != len(fields) ||
		result.ParseTelemetry.OutputFieldCount <= result.ParseTelemetry.SourceFieldCount {
		t.Fatalf("semantic field accounting is inconsistent: %+v fields=%#v", result.ParseTelemetry, fields)
	}
}

func TestSemanticExtractionDecodesEncodedSubjectFromParsedValueField(t *testing.T) {
	pack := productionTestPack([]Stage{
		{ID: "value", Name: "Capture encoded subject value", Type: StageTypeRegex, Config: map[string]any{"pattern": `^value=(?P<value>.*)$`}, FailurePolicy: FailurePolicyRejectRule},
		{ID: "subject", Name: "Decode encoded subject value", Type: StageTypeSemanticExtract, Config: map[string]any{"input_field": "value", "decode_mime_headers": true}, FailurePolicy: FailurePolicyRejectRule},
	})
	pack.Rules[0].CountPolicy = CountPolicyAllowDerived
	pack.Rules[0].FailurePolicy = FailurePolicyRejectRule
	if err := SealParserPack(pack); err != nil {
		t.Fatal(err)
	}
	executor, err := NewParserPackExecutor(pack)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(&ExecutionRequest{
		Input: "value==?UTF-8?B?U3VtbWVyIGlzbuKAmXQgb3Zlci4=?=", Service: "smtp", NodeID: "test", Context: context.Background(),
	})
	if err != nil || result.Quarantined {
		t.Fatalf("execution: result=%+v err=%v", result, err)
	}
	assertSemanticValues(t, result.EventPayload.Fields, "email_subject_decoded", []interface{}{"Summer isn’t over."})
	if result.ParseTelemetry.SourceFieldCount != 1 || result.ParseTelemetry.OutputFieldCount != 2 {
		t.Fatalf("encoded subject field accounting is inconsistent: %+v", result.ParseTelemetry)
	}
}

func assertSemanticValues(t *testing.T, fields map[string]interface{}, name string, expected []interface{}) {
	t.Helper()
	actual, exists := fields[name]
	if !exists || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s = %#v, want %#v (all fields: %#v)", name, actual, expected, fields)
	}
}
