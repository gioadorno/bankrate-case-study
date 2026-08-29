package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrMissingIntakeID       = errors.New("missing intake id")
	ErrMissingMemberID       = errors.New("missing member id")
	ErrMissingText           = errors.New("missing intake text")
	ErrUnsafeComplianceDraft = errors.New("Compliance decision produced a member draft")
	ErrDraftForbidden        = errors.New("draft forbidden by category policy")
	ErrApprovalGateMissing   = errors.New("member-facing draft missing human approval gate")
)

const (
	classifierReasonFAQMatch                = "faq_match"
	classifierReasonProductFeedbackLanguage = "product_feedback_language"
	classifierReasonComplianceLanguage      = "compliance_language"
)

type Service struct {
	safety     SafetyDetector
	classifier Classifier
	policies   PolicyRegistry
	knowledge  KnowledgeBase
	drafter    Drafter
	router     Router
	audits     AuditStore
	sanitizer  Sanitizer
	now        func() time.Time
}

func NewService(
	safety SafetyDetector,
	classifier Classifier,
	policies PolicyRegistry,
	knowledge KnowledgeBase,
	drafter Drafter,
	router Router,
	audits AuditStore,
	sanitizer Sanitizer,
) *Service {
	return &Service{
		safety: safety, classifier: classifier, policies: policies,
		knowledge: knowledge, drafter: drafter, router: router,
		audits: audits, sanitizer: sanitizer, now: time.Now,
	}
}

func (s *Service) Triage(ctx context.Context, intake Intake) (Decision, error) {
	if err := validateIntake(intake); err != nil {
		return Decision{}, err
	}

	decisionID := fmt.Sprintf("decision-%d", s.now().UnixNano())

	safety, err := s.safety.Assess(ctx, intake)
	if err != nil {
		return s.safeDegraded(ctx, decisionID, intake.ID, "safety_dependency_failure")
	}

	classification, err := s.classifier.Classify(ctx, intake)
	if err != nil {
		return s.safeDegraded(ctx, decisionID, intake.ID, "classifier_unavailable")
	}
	if !validClassifierConfidence(classification.Confidence) {
		return s.safeDegraded(ctx, decisionID, intake.ID, "classifier_invalid_confidence")
	}
	if !validClassifierReason(classification.Category, classification.ReasonCode) {
		return s.safeDegraded(ctx, decisionID, intake.ID, "classifier_invalid_reason")
	}

	category := classification.Category
	if safety.ComplianceSensitive {
		category = CategoryCompliance
	}

	policy, ok := s.policies.Get(category)
	if !ok {
		return s.safeDegraded(ctx, decisionID, intake.ID, "unknown_category")
	}

	decision := Decision{
		DecisionID:  decisionID,
		IntakeID:    intake.ID,
		Category:    category,
		Confidence:  classification.Confidence,
		ReasonCodes: []string{classification.ReasonCode},
	}
	decision.ReasonCodes = append(decision.ReasonCodes, safety.ReasonCodes...)

	if safety.ComplianceSensitive {
		decision.ReasonCodes = append(decision.ReasonCodes, "safety_override_compliance")
	}

	if !safety.ComplianceSensitive && classification.Confidence < policy.MinimumConfidence {
		return s.safeDegradedWithClassification(ctx, decision, safety, "low_confidence")
	}

	// Build the decision first. No external side effect is performed until the
	// decision passes the egress guard and a sanitized pre-execution audit record exists.
	switch policy.Action {
	case ActionDraft:
		if category == CategoryCompliance {
			return s.safeDegradedWithClassification(ctx, decision, safety, "compliance_draft_forbidden")
		}
		if !policy.DraftAllowed || safety.ComplianceSensitive {
			return s.safeDegradedWithClassification(ctx, decision, safety, "draft_not_permitted")
		}
		docs, err := s.knowledge.Retrieve(ctx, intake.Text)
		if err != nil {
			return s.safeDegradedWithClassification(ctx, decision, safety, "knowledge_unavailable")
		}
		draft, err := s.drafter.Draft(ctx, intake, docs)
		if err != nil {
			return s.safeDegradedWithClassification(ctx, decision, safety, "drafter_unavailable")
		}
		draftSafety, err := s.safety.Assess(ctx, Intake{Text: draft})
		if err != nil {
			return s.safeDegradedWithClassification(ctx, decision, safety, "draft_safety_unavailable")
		}
		if draftSafety.ComplianceSensitive || draftSafety.SensitiveDataFound {
			return s.safeDegradedWithClassification(ctx, decision, safety, "unsafe_generated_draft")
		}
		decision.Action = ActionDraft
		decision.DraftResponse = &draft
		decision.HumanApprovalRequired = true
		decision.ReasonCodes = append(decision.ReasonCodes, "policy_draft_general_qa")

	case ActionRoute:
		routingContext := RoutingContext{
			CaseID:      intake.ID,
			Summary:     s.sanitizer.SanitizeText(intake.Text),
			ReasonCodes: append([]string(nil), decision.ReasonCodes...),
			Metadata:    s.sanitizer.AllowedMetadata(intake.Metadata),
		}
		decision.Action = ActionRoute
		decision.RoutingContext = &routingContext
		decision.HumanApprovalRequired = false
		decision.ReasonCodes = append(decision.ReasonCodes, "policy_route_"+policy.Destination)

	default:
		return s.safeDegradedWithClassification(ctx, decision, safety, "unsupported_action")
	}

	if err := validateDecision(decision, safety, policy); err != nil {
		return s.safeDegradedWithClassification(ctx, decision, safety, "egress_safety_failure")
	}

	// A pre-execution audit write is a structural gate before external routing.
	// If it fails, the existing case remains in the manual workflow and no route occurs.
	preAudit := s.auditRecord(decision, safety)
	if decision.Action == ActionRoute {
		preAudit.AuditStatus = AuditPendingExecution
	} else {
		preAudit.AuditStatus = AuditPendingApproval
	}
	if err := s.audits.Save(ctx, preAudit); err != nil {
		return manualFallbackNoAudit(decision, "audit_unavailable"), nil
	}
	decision.AuditID = decision.DecisionID

	if decision.Action == ActionRoute {
		if err := s.router.Route(ctx, policy.Destination, *decision.RoutingContext); err != nil {
			fallback := manualFallback(decision, "routing_unavailable")
			degradedAudit := s.auditRecord(fallback, safety)
			degradedAudit.AuditStatus = AuditDegraded
			_ = s.audits.Save(ctx, degradedAudit)
			return fallback, nil
		}

		completedAudit := s.auditRecord(decision, safety)
		completedAudit.AuditStatus = AuditCompleted
		_ = s.audits.Save(ctx, completedAudit)
	}

	return decision, nil
}

func validateIntake(in Intake) error {
	if strings.TrimSpace(in.ID) == "" {
		return ErrMissingIntakeID
	}
	if strings.TrimSpace(in.MemberID) == "" {
		return ErrMissingMemberID
	}
	if strings.TrimSpace(in.Text) == "" {
		return ErrMissingText
	}
	return nil
}

func validClassifierConfidence(confidence float64) bool {
	return !math.IsNaN(confidence) && !math.IsInf(confidence, 0) && confidence >= 0 && confidence <= 1
}

func validClassifierReason(category Category, reason string) bool {
	switch category {
	case CategoryGeneralQA:
		return reason == classifierReasonFAQMatch
	case CategoryProductFeedback:
		return reason == classifierReasonProductFeedbackLanguage
	case CategoryCompliance:
		return reason == classifierReasonComplianceLanguage
	default:
		return false
	}
}

func validateDecision(decision Decision, safety SafetyAssessment, policy CategoryPolicy) error {
	if (decision.Category == CategoryCompliance || safety.ComplianceSensitive) && decision.DraftResponse != nil {
		return ErrUnsafeComplianceDraft
	}
	if !policy.DraftAllowed && decision.DraftResponse != nil {
		return ErrDraftForbidden
	}
	if decision.DraftResponse != nil && !decision.HumanApprovalRequired {
		return ErrApprovalGateMissing
	}
	return nil
}

func (s *Service) safeDegraded(ctx context.Context, decisionID, intakeID, reason string) (Decision, error) {
	decision := Decision{
		DecisionID:            decisionID,
		IntakeID:              intakeID,
		Category:              CategoryUnknown,
		Action:                ActionEscalate,
		Confidence:            0,
		HumanApprovalRequired: false,
		ReasonCodes:           []string{reason, "degraded_mode"},
	}
	_ = s.audits.Save(ctx, s.auditRecord(decision, SafetyAssessment{}))
	return decision, nil
}

func (s *Service) safeDegradedWithClassification(ctx context.Context, base Decision, safety SafetyAssessment, reason string) (Decision, error) {
	base = manualFallback(base, reason)
	record := s.auditRecord(base, safety)
	record.AuditStatus = AuditDegraded
	_ = s.audits.Save(ctx, record)
	return base, nil
}

func manualFallback(base Decision, reason string) Decision {
	base.Action = ActionEscalate
	base.DraftResponse = nil
	base.RoutingContext = nil
	base.HumanApprovalRequired = false
	base.ReasonCodes = append(base.ReasonCodes, reason, "degraded_mode")
	return base
}

func manualFallbackNoAudit(base Decision, reason string) Decision {
	base = manualFallback(base, reason)
	base.AuditID = ""
	return base
}

func (s *Service) auditRecord(decision Decision, safety SafetyAssessment) AuditRecord {
	status := ApprovalNotApplicable
	if decision.DraftResponse != nil {
		status = ApprovalPending
	}
	record := AuditRecord{
		DecisionID:     decision.DecisionID,
		IntakeID:       decision.IntakeID,
		Category:       decision.Category,
		Action:         decision.Action,
		Confidence:     decision.Confidence,
		PolicyVersion:  s.policies.Version(),
		ReasonCodes:    append([]string(nil), decision.ReasonCodes...),
		SafetyFlags:    append([]string(nil), safety.ReasonCodes...),
		ApprovalStatus: status,
		AuditStatus:    AuditDegraded,
		CreatedAt:      s.now(),
	}
	if decision.DraftResponse != nil {
		hash := sha256.Sum256([]byte(*decision.DraftResponse))
		record.DraftSHA256 = hex.EncodeToString(hash[:])
	}
	return record
}
