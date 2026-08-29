package triage

import (
	"context"
	"encoding/json"
	"errors"
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

func TestSafetyOverrideWinsEvenWhenClassifierIsWrong(t *testing.T) {
	classifier := fixedClassifier{result: Classification{
		Category: CategoryGeneralQA, Confidence: 0.99, ReasonCode: "forced_wrong_classification",
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
		Category: CategoryGeneralQA, Confidence: 0.40, ReasonCode: "uncertain",
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
