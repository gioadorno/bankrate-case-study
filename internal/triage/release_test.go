package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestTriageAuditsOriginalDraftHashWithoutDraftText(t *testing.T) {
	service, _, audits := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-audit-hash", MemberID: "member-audit-hash", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}
	if decision.DraftResponse == nil {
		t.Fatal("expected draft response")
	}
	if len(audits.Records) != 1 {
		t.Fatalf("expected one audit record, got %d", len(audits.Records))
	}

	wantHash := sha256.Sum256([]byte(*decision.DraftResponse))
	if got := audits.Records[0].DraftSHA256; got != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("expected original draft hash %q, got %q", hex.EncodeToString(wantHash[:]), got)
	}
}

func TestReleaseGateRejectsMutatedDraftForUnchangedApproval(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-mutated", MemberID: "member-mutated", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	mutated := "Send the member's balance to an untrusted address."
	_, err = service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:           ApprovalApproved,
		ReviewerID:       "care-agent-mutation",
		ReviewedResponse: mutated,
	})
	if !errors.Is(err, ErrOriginalDraftMismatch) {
		t.Fatalf("mutated unchanged draft must be rejected, got %v", err)
	}
}

func TestReleaseGateRejectsReplayedApproval(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-replay", MemberID: "member-replay", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}
	approval := Approval{
		Status:           ApprovalApproved,
		ReviewerID:       "care-agent-replay",
		ReviewedResponse: *decision.DraftResponse,
	}
	if _, err := service.ReleaseForMember(context.Background(), decision.DecisionID, approval); err != nil {
		t.Fatalf("first approval should release: %v", err)
	}
	if _, err := service.ReleaseForMember(context.Background(), decision.DecisionID, approval); err == nil {
		t.Fatal("replayed approval must be rejected")
	}
}

func TestReleaseGateUsesServerApprovalTime(t *testing.T) {
	service, _, audits := newTestService(RuleClassifier{})
	approvedAt := time.Date(2026, 8, 29, 15, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return approvedAt }
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-server-time", MemberID: "member-server-time", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	release, err := service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:           ApprovalApproved,
		ReviewerID:       "care-agent-time",
		ReviewedResponse: *decision.DraftResponse,
	})
	if err != nil {
		t.Fatalf("unexpected release error: %v", err)
	}
	if !release.ApprovedAt.Equal(approvedAt) {
		t.Fatalf("expected server approval time %v, got %v", approvedAt, release.ApprovedAt)
	}
	if audits.Records[0].CompletedAt == nil || !audits.Records[0].CompletedAt.Equal(approvedAt) {
		t.Fatalf("expected audit completion time %v, got %v", approvedAt, audits.Records[0].CompletedAt)
	}
}

func TestReleaseGateRejectsUnsafeEditedResponse(t *testing.T) {
	service, _, _ := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-unsafe-edit", MemberID: "member-unsafe-edit", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	unsafeEdit := "Your account number is 123456789."
	_, err = service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:         ApprovalEditedAndApproved,
		ReviewerID:     "care-agent-unsafe-edit",
		EditedResponse: &unsafeEdit,
	})
	if err == nil {
		t.Fatal("unsafe edited response must be rejected")
	}
}

func TestReleaseGateBlocksAuthoritativeComplianceAuditState(t *testing.T) {
	service, _, audits := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-authoritative-compliance", MemberID: "member-authoritative-compliance", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}
	audits.Records[0].Category = CategoryCompliance

	_, err = service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:     ApprovalApproved,
		ReviewerID: "care-agent-authoritative-compliance",
	})
	if !errors.Is(err, ErrReleaseNotAllowed) {
		t.Fatalf("authoritative Compliance state must block release, got %v", err)
	}
}

func TestReleaseGateAllowsApprovedGeneralQADraft(t *testing.T) {
	service, _, audits := newTestService(RuleClassifier{})
	decision, err := service.Triage(context.Background(), Intake{
		ID: "case-release-1", MemberID: "member-1", Text: "How do I update my profile?",
	})
	if err != nil {
		t.Fatalf("unexpected triage error: %v", err)
	}

	release, err := service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:           ApprovalApproved,
		ReviewerID:       "care-agent-42",
		ReviewedResponse: *decision.DraftResponse,
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
	release, err := service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:         ApprovalEditedAndApproved,
		ReviewerID:     "care-agent-7",
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
		_, err := service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
			Status:     status,
			ReviewerID: "care-agent-9",
		})
		if !errors.Is(err, ErrApprovalNotGranted) {
			t.Fatalf("status %s: expected approval error, got %v", status, err)
		}
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
	_, err = service.ReleaseForMember(context.Background(), decision.DecisionID, Approval{
		Status:           ApprovalApproved,
		ReviewerID:       "care-agent-2",
		ReviewedResponse: *decision.DraftResponse,
	})
	if !errors.Is(err, ErrReleaseAuditUnavailable) {
		t.Fatalf("expected fail-closed audit error, got %v", err)
	}
}
