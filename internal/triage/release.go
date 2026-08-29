package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrReleaseNotAllowed        = errors.New("decision is not eligible for member release")
	ErrApprovalNotGranted       = errors.New("human approval not granted")
	ErrReviewerRequired         = errors.New("reviewer id is required")
	ErrReviewedResponseRequired = errors.New("reviewed response is required")
	ErrOriginalDraftMismatch    = errors.New("reviewed response does not match original draft")
	ErrEditedResponseRequired   = errors.New("edited response is required")
	ErrEditedResponseUnsafe     = errors.New("edited response is unsafe for member release")
	ErrReleaseAuditUnavailable  = errors.New("approval audit could not be persisted")
	ErrApprovalStateConflict    = errors.New("approval is no longer eligible")
)

// ReleaseForMember is the final structural gate before the existing member-send
// workflow. It does not send anything itself; it returns a releasable payload only
// after the authoritative audit state is eligible, a human approval is valid,
// and the approval is durably recorded.
func (s *Service) ReleaseForMember(ctx context.Context, decisionID string, approval Approval) (MemberRelease, error) {
	audit, err := s.audits.Get(ctx, decisionID)
	if err != nil {
		return MemberRelease{}, ErrReleaseAuditUnavailable
	}

	if audit.Category != CategoryGeneralQA ||
		audit.Action != ActionDraft ||
		audit.AuditStatus != AuditPendingApproval ||
		audit.ApprovalStatus != ApprovalPending ||
		strings.TrimSpace(audit.DraftSHA256) == "" {
		return MemberRelease{}, ErrReleaseNotAllowed
	}
	if strings.TrimSpace(approval.ReviewerID) == "" {
		return MemberRelease{}, ErrReviewerRequired
	}

	var response string
	switch approval.Status {
	case ApprovalApproved:
		if strings.TrimSpace(approval.ReviewedResponse) == "" {
			return MemberRelease{}, ErrReviewedResponseRequired
		}
		draftHash := sha256.Sum256([]byte(approval.ReviewedResponse))
		if hex.EncodeToString(draftHash[:]) != audit.DraftSHA256 {
			return MemberRelease{}, ErrOriginalDraftMismatch
		}
		response = approval.ReviewedResponse
	case ApprovalEditedAndApproved:
		if approval.EditedResponse == nil || strings.TrimSpace(*approval.EditedResponse) == "" {
			return MemberRelease{}, ErrEditedResponseRequired
		}
		assessment, err := s.safety.Assess(ctx, Intake{Text: *approval.EditedResponse})
		if err != nil || assessment.ComplianceSensitive || assessment.SensitiveDataFound {
			return MemberRelease{}, ErrEditedResponseUnsafe
		}
		response = *approval.EditedResponse
	default:
		return MemberRelease{}, ErrApprovalNotGranted
	}

	// Audit persistence is part of the release boundary. If the approval cannot
	// be recorded, no releasable member payload is returned.
	approvedAt := s.now()
	if err := s.audits.CompareAndSetApproval(
		ctx,
		decisionID,
		audit.DraftSHA256,
		approval.Status,
		approval.ReviewerID,
		approvedAt,
	); err != nil {
		if errors.Is(err, ErrApprovalStateConflict) {
			return MemberRelease{}, err
		}
		return MemberRelease{}, ErrReleaseAuditUnavailable
	}

	return MemberRelease{
		DecisionID: decisionID,
		Response:   response,
		ApprovedBy: approval.ReviewerID,
		ApprovedAt: approvedAt,
	}, nil
}
