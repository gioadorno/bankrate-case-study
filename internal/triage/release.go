package triage

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrReleaseNotAllowed        = errors.New("decision is not eligible for member release")
	ErrApprovalDecisionMismatch = errors.New("approval does not match decision")
	ErrApprovalNotGranted       = errors.New("human approval not granted")
	ErrReviewerRequired         = errors.New("reviewer id is required")
	ErrReviewTimeRequired       = errors.New("review time is required")
	ErrEditedResponseRequired   = errors.New("edited response is required")
	ErrReleaseAuditUnavailable  = errors.New("approval audit could not be persisted")
)

// ReleaseForMember is the final structural gate before the existing member-send
// workflow. It does not send anything itself; it returns a releasable payload only
// after the decision is eligible, a human approval is valid, and the approval is
// durably recorded.
func (s *Service) ReleaseForMember(ctx context.Context, decision Decision, approval Approval) (MemberRelease, error) {
	if decision.Category != CategoryGeneralQA ||
		decision.Action != ActionDraft ||
		decision.DraftResponse == nil ||
		!decision.HumanApprovalRequired {
		return MemberRelease{}, ErrReleaseNotAllowed
	}

	if approval.DecisionID != decision.DecisionID {
		return MemberRelease{}, ErrApprovalDecisionMismatch
	}
	if strings.TrimSpace(approval.ReviewerID) == "" {
		return MemberRelease{}, ErrReviewerRequired
	}
	if approval.ReviewedAt.IsZero() {
		return MemberRelease{}, ErrReviewTimeRequired
	}

	response := *decision.DraftResponse
	switch approval.Status {
	case ApprovalApproved:
		// Release the reviewed draft unchanged.
	case ApprovalEditedAndApproved:
		if approval.EditedResponse == nil || strings.TrimSpace(*approval.EditedResponse) == "" {
			return MemberRelease{}, ErrEditedResponseRequired
		}
		response = *approval.EditedResponse
	default:
		return MemberRelease{}, ErrApprovalNotGranted
	}

	// Audit persistence is part of the release boundary. If the approval cannot
	// be recorded, no releasable member payload is returned.
	if err := s.audits.UpdateApproval(
		ctx,
		decision.DecisionID,
		approval.Status,
		approval.ReviewerID,
		approval.ReviewedAt,
	); err != nil {
		return MemberRelease{}, ErrReleaseAuditUnavailable
	}

	return MemberRelease{
		DecisionID: decision.DecisionID,
		Response:   response,
		ApprovedBy: approval.ReviewerID,
		ApprovedAt: approval.ReviewedAt,
	}, nil
}
