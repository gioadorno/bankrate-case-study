package triage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReleaseGateAllowsApprovedGeneralQADraft(t *testing.T) {
	service, _, audits := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-1", MemberID: "member-1", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	reviewedAt := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	release, err := service.ReleaseForMember(context.Background(), decision, Approval{
		DecisionID: decision.DecisionID,
		Status:     ApprovalApproved,
		ReviewerID: "care-agent-42",
		ReviewedAt: reviewedAt,
	})
	if err != nil {
		t.Fatalf("unexpected release error: %v", err)
	}
	if release.Response != *decision.DraftResponse {
		t.Fatal("approved release should use the reviewed draft")
	}
	if release.ApprovedBy != "care-agent-42" {
		t.Fatalf("expected approver to be recorded, got %q", release.ApprovedBy)
	}
	if len(audits.Records) != 1 {
		t.Fatalf("expected one audit record, got %d", len(audits.Records))
	}
	if audits.Records[0].ApprovalStatus != ApprovalApproved || audits.Records[0].ApprovedBy != "care-agent-42" {
		t.Fatal("audit record must contain approval status and approver")
	}
	if audits.Records[0].AuditStatus != AuditCompleted || audits.Records[0].CompletedAt == nil {
		t.Fatal("approved release must complete the audit record")
	}
}

func TestReleaseGateSupportsEditedAndApprovedResponse(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-2", MemberID: "member-2", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	edited := "You can update your profile from Account Settings."
	release, err := service.ReleaseForMember(context.Background(), decision, Approval{
		DecisionID:     decision.DecisionID,
		Status:         ApprovalEditedAndApproved,
		ReviewerID:     "care-agent-7",
		ReviewedAt:     time.Now(),
		EditedResponse: &edited,
	})
	if err != nil {
		t.Fatalf("unexpected release error: %v", err)
	}
	if release.Response != edited {
		t.Fatalf("expected edited response, got %q", release.Response)
	}
}

func TestReleaseGateBlocksPendingOrRejectedApproval(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-3", MemberID: "member-3", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	for _, status := range []ApprovalStatus{ApprovalPending, ApprovalRejected} {
		_, err := service.ReleaseForMember(context.Background(), decision, Approval{
			DecisionID: decision.DecisionID,
			Status:     status,
			ReviewerID: "care-agent-9",
			ReviewedAt: time.Now(),
		})
		if !errors.Is(err, ErrApprovalNotGranted) {
			t.Fatalf("status %s: expected approval error, got %v", status, err)
		}
	}
}

func TestReleaseGateBlocksComplianceEvenWithForgedApproval(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	unsafeDraft := "This should never be releasable."
	forged := Decision{
		DecisionID:            "forged-compliance-decision",
		IntakeID:              "case-release-4",
		Category:              CategoryCompliance,
		Action:                ActionDraft,
		DraftResponse:         &unsafeDraft,
		HumanApprovalRequired: true,
	}

	_, err := service.ReleaseForMember(context.Background(), forged, Approval{
		DecisionID: forged.DecisionID,
		Status:     ApprovalApproved,
		ReviewerID: "care-agent-1",
		ReviewedAt: time.Now(),
	})
	if !errors.Is(err, ErrReleaseNotAllowed) {
		t.Fatalf("expected structural release block, got %v", err)
	}
}

func TestReleaseGateFailsClosedWhenApprovalAuditCannotPersist(t *testing.T) {
	service, _, audits := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-5", MemberID: "member-5", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	audits.Err = errors.New("audit unavailable")
	_, err = service.ReleaseForMember(context.Background(), decision, Approval{
		DecisionID: decision.DecisionID,
		Status:     ApprovalApproved,
		ReviewerID: "care-agent-2",
		ReviewedAt: time.Now(),
	})
	if !errors.Is(err, ErrReleaseAuditUnavailable) {
		t.Fatalf("expected fail-closed audit error, got %v", err)
	}
}
