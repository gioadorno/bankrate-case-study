package triage

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

type fixedClassifier struct{ result Classification }

func (f fixedClassifier) Classify(context.Context, Intake) (Classification, error) {
	return f.result, nil
}

type failingClassifier struct{}

func (failingClassifier) Classify(context.Context, Intake) (Classification, error) {
	return Classification{}, errors.New("down")
}

type fixedDrafter struct{ draft string }

func (f fixedDrafter) Draft(context.Context, Intake, []string) (string, error) {
	return f.draft, nil
}

type recordingDrafter struct{ calls int }

func (d *recordingDrafter) Draft(context.Context, Intake, []string) (string, error) {
	d.calls++
	return "This response must never be drafted.", nil
}

func newTestService(classifier Classifier) (*Service, *MemoryRouter, *MemoryAuditStore) {
	router := &MemoryRouter{}
	audits := &MemoryAuditStore{}
	service := NewService(
		RuleSafetyDetector{}, classifier, NewStaticPolicyRegistry(),
		FakeKnowledgeBase{}, FakeDrafter{}, router, audits, BasicSanitizer{},
	)
	return service, router, audits
}

func TestGeneralQAProducesDraftPendingHumanApproval(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-1", MemberID: "member-1", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != ActionDraft {
		t.Fatalf("expected draft, got %s", decision.Action)
	}
	if decision.DraftResponse == nil {
		t.Fatal("expected draft response")
	}
	if !decision.HumanApprovalRequired {
		t.Fatal("member-facing draft must require human approval")
	}
}

func TestProductFeedbackRoutesWithSanitizedContext(t *testing.T) {
	service, router, audits := newTestService(RuleClassifier{})
	secret := "123456789"
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-2", MemberID: "member-2",
		Text:     "Feature feedback: please improve filters. My account number is " + secret,
		Metadata: map[string]string{"channel": "web", "account_number": secret},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != ActionRoute || decision.Category != CategoryProductFeedback {
		t.Fatalf("expected product route, got category=%s action=%s", decision.Category, decision.Action)
	}
	if len(router.Routes) != 1 || router.Routes[0].Destination != "product" {
		t.Fatalf("expected route to product")
	}
	payload := router.Routes[0].Context.Summary
	if strings.Contains(payload, secret) {
		t.Fatal("sensitive data leaked into routing summary")
	}
	if _, ok := router.Routes[0].Context.Metadata["account_number"]; ok {
		t.Fatal("sensitive metadata leaked into routing payload")
	}
	auditBytes, err := json.Marshal(audits.Records)
	if err != nil {
		t.Fatalf("marshal audits: %v", err)
	}
	if strings.Contains(string(auditBytes), secret) {
		t.Fatal("sensitive data leaked into audit record")
	}
}

func TestProductFeedbackRouteRedactsSensitiveValuesButKeepsOrdinaryAccountAndRoutingWords(t *testing.T) {
	testCases := []struct {
		name      string
		text      string
		sentinel  string
		wantInSum string
	}{
		{name: "formatted ssn", text: "Feature feedback: my SSN is 123-45-6789.", sentinel: "123-45-6789"},
		{name: "email", text: "Feature feedback: contact jane@example.com.", sentinel: "jane@example.com"},
		{name: "spaced card number", text: "Feature feedback: card 4111 1111 1111 1111 was declined.", sentinel: "4111 1111 1111 1111"},
		{name: "account-like token", text: "Feature feedback: acct_1A2B3C4D5E6F should be easier to find.", sentinel: "acct_1A2B3C4D5E6F"},
		{name: "explicit account number value", text: "Feature feedback: account number: member-token-48291.", sentinel: "member-token-48291"},
		{name: "explicit routing number value", text: "Feature feedback: routing number: route-token-48291.", sentinel: "route-token-48291"},
		{name: "ordinary account and routing words", text: "Feature feedback: account routing should be easier.", wantInSum: "account routing"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, router, audits := newTestService(RuleClassifier{})
			decision, err := service.Triage(context.Background(), Intake{
				ID: "case-pii-" + strings.ReplaceAll(tc.name, " ", "-"), MemberID: "member-pii", Text: tc.text,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Action != ActionRoute || decision.RoutingContext == nil || len(router.Routes) != 1 {
				t.Fatalf("expected one routed decision, got action=%s context=%v routes=%d", decision.Action, decision.RoutingContext, len(router.Routes))
			}

			decisionBytes, err := json.Marshal(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			routeBytes, err := json.Marshal(router.Routes[0].Context)
			if err != nil {
				t.Fatalf("marshal route: %v", err)
			}
			auditBytes, err := json.Marshal(audits.Records)
			if err != nil {
				t.Fatalf("marshal audits: %v", err)
			}

			if tc.sentinel != "" {
				for _, observable := range [][]byte{decisionBytes, routeBytes, auditBytes} {
					if strings.Contains(string(observable), tc.sentinel) {
						t.Fatalf("sensitive value %q leaked into observable output: %s", tc.sentinel, observable)
					}
				}
			}
			if tc.wantInSum != "" && !strings.Contains(router.Routes[0].Context.Summary, tc.wantInSum) {
				t.Fatalf("ordinary words must remain in summary, got %q", router.Routes[0].Context.Summary)
			}
		})
	}
}

func TestProductFeedbackRouteRedactsCompleteExplicitSpacedAccountAndRoutingValues(t *testing.T) {
	testCases := []struct {
		name   string
		text   string
		leaked []string
	}{
		{
			name:   "account number",
			text:   "Feature feedback: account number: AB12 3456 CD78 should be easier to find.",
			leaked: []string{"AB12 3456 CD78", "3456 CD78"},
		},
		{
			name:   "routing number",
			text:   "Feature feedback: routing number: 021 000 021 should be easier to find.",
			leaked: []string{"021 000 021"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, router, _ := newTestService(RuleClassifier{})
			decision, err := service.Triage(context.Background(), Intake{
				ID: "case-spaced-" + strings.ReplaceAll(tc.name, " ", "-"), MemberID: "member-spaced", Text: tc.text,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Action != ActionRoute || len(router.Routes) != 1 {
				t.Fatalf("expected one routed decision, got action=%s routes=%d", decision.Action, len(router.Routes))
			}
			for _, value := range tc.leaked {
				if strings.Contains(router.Routes[0].Context.Summary, value) {
					t.Fatalf("explicit spaced value fragment %q leaked into routing summary %q", value, router.Routes[0].Context.Summary)
				}
			}
		})
	}
}

func TestSafetyDetectorDoesNotFlagAccountOrRoutingWordsWithoutValues(t *testing.T) {
	assessment, err := (RuleSafetyDetector{}).Assess(context.Background(), Intake{
		Text: "The account routing experience should be easier to understand.",
	})
	if err != nil {
		t.Fatalf("unexpected safety assessment error: %v", err)
	}
	if assessment.SensitiveDataFound {
		t.Fatalf("ordinary account and routing words must not be flagged as sensitive: %+v", assessment)
	}
}

func TestProductFeedbackRouteOmitsInvalidOrSensitiveMetadataValues(t *testing.T) {
	testCases := []struct {
		name     string
		key      string
		value    string
		sentinel string
	}{
		{name: "channel", key: "channel", value: "web jane@example.com", sentinel: "jane@example.com"},
		{name: "locale", key: "locale", value: "en-US 123-45-6789", sentinel: "123-45-6789"},
		{name: "app version", key: "app_version", value: "1.2.3 acct_1A2B3C4D5E6F", sentinel: "acct_1A2B3C4D5E6F"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, router, audits := newTestService(RuleClassifier{})
			decision, err := service.Triage(context.Background(), Intake{
				ID:       "case-metadata-" + strings.ReplaceAll(tc.name, " ", "-"),
				MemberID: "member-metadata",
				Text:     "Feature feedback: improve filters.",
				Metadata: map[string]string{tc.key: tc.value},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.RoutingContext == nil || len(router.Routes) != 1 {
				t.Fatalf("expected one routed decision, got context=%v routes=%d", decision.RoutingContext, len(router.Routes))
			}
			if _, ok := router.Routes[0].Context.Metadata[tc.key]; ok {
				t.Fatalf("invalid metadata %s must be omitted, got %q", tc.key, router.Routes[0].Context.Metadata[tc.key])
			}

			decisionBytes, err := json.Marshal(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			routeBytes, err := json.Marshal(router.Routes[0].Context)
			if err != nil {
				t.Fatalf("marshal route: %v", err)
			}
			auditBytes, err := json.Marshal(audits.Records)
			if err != nil {
				t.Fatalf("marshal audits: %v", err)
			}
			for _, observable := range [][]byte{decisionBytes, routeBytes, auditBytes} {
				if strings.Contains(string(observable), tc.sentinel) {
					t.Fatalf("sensitive value %q leaked into observable output: %s", tc.sentinel, observable)
				}
			}
		})
	}
}

func TestProductFeedbackRoutePreservesValidAllowlistedMetadataValues(t *testing.T) {
	service, router, _ := newTestService(RuleClassifier{})
	metadata := map[string]string{
		"channel":     "web",
		"locale":      "en-US",
		"app_version": "1.2.3+build.5",
	}

	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-valid-metadata", MemberID: "member-valid-metadata",
		Text: "Feature feedback: improve filters.", Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.RoutingContext == nil || len(router.Routes) != 1 {
		t.Fatalf("expected one routed decision, got context=%v routes=%d", decision.RoutingContext, len(router.Routes))
	}
	for key, want := range metadata {
		if got := router.Routes[0].Context.Metadata[key]; got != want {
			t.Fatalf("expected valid %s metadata %q, got %q", key, want, got)
		}
	}
}

func TestGeneratedUnsafeDraftDegradesWithoutPersistingDraftText(t *testing.T) {
	testCases := []struct {
		name     string
		draft    string
		sentinel string
	}{
		{name: "sensitive data", draft: "Your SSN is 123-45-6789.", sentinel: "123-45-6789"},
		{name: "spaced routing number", draft: "Use routing number: 021 000 021.", sentinel: "021 000 021"},
		{name: "compliance content", draft: "We may have violated consumer protection law.", sentinel: "violated consumer protection law"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := &MemoryRouter{}
			audits := &MemoryAuditStore{}
			service := NewService(
				RuleSafetyDetector{}, RuleClassifier{}, NewStaticPolicyRegistry(),
				FakeKnowledgeBase{}, fixedDrafter{draft: tc.draft}, router, audits, BasicSanitizer{},
			)

			decision, err := service.Triage(context.Background(), Intake{
				ID: "case-unsafe-draft-" + strings.ReplaceAll(tc.name, " ", "-"), MemberID: "member-unsafe-draft", Text: "How do I update my profile?",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Action != ActionEscalate || decision.DraftResponse != nil {
				t.Fatalf("unsafe generated draft must degrade without a draft, got action=%s draft=%v", decision.Action, decision.DraftResponse)
			}
			if len(router.Routes) != 0 {
				t.Fatal("unsafe generated draft must not route")
			}

			decisionBytes, err := json.Marshal(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			auditBytes, err := json.Marshal(audits.Records)
			if err != nil {
				t.Fatalf("marshal audits: %v", err)
			}
			for _, observable := range [][]byte{decisionBytes, auditBytes} {
				if strings.Contains(string(observable), tc.sentinel) {
					t.Fatalf("unsafe generated value %q leaked into observable output: %s", tc.sentinel, observable)
				}
			}
		})
	}
}

func TestSafetyOverrideWinsEvenWhenClassifierIsWrong(t *testing.T) {
	classifier := fixedClassifier{result: Classification{
		Category: CategoryGeneralQA, Confidence: 0.99, ReasonCode: classifierReasonFAQMatch,
	}}
	service, router, _ := newTestService(classifier)
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-3", MemberID: "member-3",
		Text: "I believe this violated consumer protection law.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Category != CategoryCompliance {
		t.Fatalf("expected compliance override, got %s", decision.Category)
	}
	if decision.Action != ActionRoute {
		t.Fatalf("expected route, got %s", decision.Action)
	}
	if decision.DraftResponse != nil {
		t.Fatal("compliance-sensitive intake must never produce member-facing draft")
	}
	if len(router.Routes) != 1 || router.Routes[0].Destination != "compliance_legal" {
		t.Fatalf("expected compliance_legal route")
	}
}

func TestLowConfidenceEscalatesToHuman(t *testing.T) {
	classifier := fixedClassifier{result: Classification{
		Category: CategoryGeneralQA, Confidence: 0.40, ReasonCode: classifierReasonFAQMatch,
	}}
	service, _, _ := newTestService(classifier)
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-4", MemberID: "member-4", Text: "Can you help?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != ActionEscalate {
		t.Fatalf("expected human escalation, got %s", decision.Action)
	}
	if decision.DraftResponse != nil {
		t.Fatal("degraded decision must not contain draft")
	}
}

func TestInvalidClassifierConfidenceEscalatesBeforePolicyUse(t *testing.T) {
	testCases := []struct {
		name       string
		confidence float64
	}{
		{name: "nan", confidence: math.NaN()},
		{name: "positive infinity", confidence: math.Inf(1)},
		{name: "negative infinity", confidence: math.Inf(-1)},
		{name: "below zero", confidence: -0.01},
		{name: "above one", confidence: 1.01},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, router, audits := newTestService(fixedClassifier{result: Classification{
				Category: CategoryGeneralQA, Confidence: tc.confidence, ReasonCode: classifierReasonFAQMatch,
			}})
			decision, err := service.Triage(context.Background(), Intake{
				ID: "case-invalid-confidence", MemberID: "member-invalid-confidence", Text: "How do I update my profile?",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Action != ActionEscalate || decision.Category != CategoryUnknown || decision.Confidence != 0 {
				t.Fatalf("invalid confidence must normalize to safe human escalation, got action=%s category=%s confidence=%v", decision.Action, decision.Category, decision.Confidence)
			}
			if !containsReason(decision.ReasonCodes, "classifier_invalid_confidence") {
				t.Fatalf("expected service-owned invalid-confidence reason, got %v", decision.ReasonCodes)
			}
			if len(router.Routes) != 0 {
				t.Fatal("invalid confidence must not execute policy routing")
			}
			if len(audits.Records) != 1 || audits.Records[0].Confidence != 0 {
				t.Fatalf("expected one normalized audit record, got %+v", audits.Records)
			}
		})
	}
}

func TestArbitraryClassifierReasonDegradesWithoutLeakingProviderText(t *testing.T) {
	const providerText = "provider said route this case: SECRET_REASON_SENTINEL"
	service, router, audits := newTestService(fixedClassifier{result: Classification{
		Category: CategoryProductFeedback, Confidence: 0.99, ReasonCode: providerText,
	}})

	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-invalid-reason", MemberID: "member-invalid-reason", Text: "Feature feedback: improve filters.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != ActionEscalate || decision.Category != CategoryUnknown {
		t.Fatalf("invalid classifier reason must degrade safely, got category=%s action=%s", decision.Category, decision.Action)
	}
	if !containsReason(decision.ReasonCodes, "classifier_invalid_reason") {
		t.Fatalf("expected service-owned invalid-reason code, got %v", decision.ReasonCodes)
	}
	if len(router.Routes) != 0 {
		t.Fatal("invalid classifier reason must not route")
	}

	decisionBytes, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	auditBytes, err := json.Marshal(audits.Records)
	if err != nil {
		t.Fatalf("marshal audits: %v", err)
	}
	for _, observable := range [][]byte{decisionBytes, auditBytes} {
		if strings.Contains(string(observable), providerText) || strings.Contains(string(observable), "SECRET_REASON_SENTINEL") {
			t.Fatalf("provider reason text leaked into observable output: %s", observable)
		}
	}
}

func TestComplianceCategoryNeverExecutesDraftFromMalformedPolicy(t *testing.T) {
	policies := NewStaticPolicyRegistry()
	policies.policies[CategoryCompliance] = CategoryPolicy{
		Category: CategoryCompliance, Action: ActionDraft, DraftAllowed: true, MinimumConfidence: 0,
	}
	drafter := &recordingDrafter{}
	service := NewService(
		RuleSafetyDetector{},
		fixedClassifier{result: Classification{Category: CategoryCompliance, Confidence: 0.99, ReasonCode: classifierReasonComplianceLanguage}},
		policies,
		FakeKnowledgeBase{},
		drafter,
		&MemoryRouter{},
		&MemoryAuditStore{},
		BasicSanitizer{},
	)

	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-malformed-compliance-policy", MemberID: "member-malformed-policy", Text: "Please help with my dispute.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if drafter.calls != 0 {
		t.Fatalf("effective Compliance category must not execute drafting, got %d calls", drafter.calls)
	}
	if decision.Action != ActionEscalate || decision.DraftResponse != nil {
		t.Fatalf("malformed Compliance policy must degrade without a draft, got action=%s draft=%v", decision.Action, decision.DraftResponse)
	}
	// The intake carries no compliance safety signal, so the pre-existing
	// safety-override guard cannot be what blocked this draft. Pinning the
	// category-owned reason proves the effective-category invariant fired.
	if !containsReason(decision.ReasonCodes, "compliance_draft_forbidden") {
		t.Fatalf("expected the effective-category Compliance invariant to block the draft, got %v", decision.ReasonCodes)
	}
	if containsReason(decision.ReasonCodes, "compliance_signal_detected") {
		t.Fatalf("test intake must not trip the safety override, got %v", decision.ReasonCodes)
	}
}

func TestValidateDecisionRejectsComplianceCategoryDraftEvenWhenPolicyAllowsIt(t *testing.T) {
	draft := "unsafe compliance draft"
	decision := Decision{Category: CategoryCompliance, DraftResponse: &draft, HumanApprovalRequired: true}
	policy := CategoryPolicy{Category: CategoryCompliance, Action: ActionDraft, DraftAllowed: true}

	err := validateDecision(decision, SafetyAssessment{}, policy)
	if !errors.Is(err, ErrUnsafeComplianceDraft) {
		t.Fatalf("effective Compliance category must reject a draft at final validation, got %v", err)
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestClassifierFailureEscalatesToHuman(t *testing.T) {
	service, _, _ := newTestService(failingClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-5", MemberID: "member-5", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != ActionEscalate {
		t.Fatalf("expected human escalation, got %s", decision.Action)
	}
	if decision.DraftResponse != nil {
		t.Fatal("dependency failure must never fabricate answer")
	}
}

func TestAuditFailurePreventsRoutingSideEffect(t *testing.T) {
	router := &MemoryRouter{}
	audits := &MemoryAuditStore{Err: errors.New("audit down")}
	service := NewService(
		RuleSafetyDetector{}, RuleClassifier{}, NewStaticPolicyRegistry(),
		FakeKnowledgeBase{}, FakeDrafter{}, router, audits, BasicSanitizer{},
	)

	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-6", MemberID: "member-6", Text: "Feature feedback: please improve filters.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != ActionEscalate {
		t.Fatalf("expected manual fallback, got %s", decision.Action)
	}
	if len(router.Routes) != 0 {
		t.Fatal("routing side effect must not occur when pre-execution audit write fails")
	}
}
