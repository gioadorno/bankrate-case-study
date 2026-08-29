package triage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type RuleClassifier struct{}

func (RuleClassifier) Classify(_ context.Context, intake Intake) (Classification, error) {
	lower := strings.ToLower(intake.Text)

	if strings.Contains(lower, "feedback") || strings.Contains(lower, "feature") || strings.Contains(lower, "wish") || strings.Contains(lower, "improve") {
		return Classification{Category: CategoryProductFeedback, Confidence: 0.90, ReasonCode: classifierReasonProductFeedbackLanguage}, nil
	}

	if strings.Contains(lower, "law") || strings.Contains(lower, "illegal") || strings.Contains(lower, "regulator") || strings.Contains(lower, "violation") {
		return Classification{Category: CategoryCompliance, Confidence: 0.82, ReasonCode: classifierReasonComplianceLanguage}, nil
	}

	return Classification{Category: CategoryGeneralQA, Confidence: 0.92, ReasonCode: classifierReasonFAQMatch}, nil
}

type FakeKnowledgeBase struct {
	Err error
}

func (f FakeKnowledgeBase) Retrieve(_ context.Context, _ string) ([]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return []string{"Members can update their profile from Account Settings."}, nil
}

type FakeDrafter struct {
	Err error
}

func (f FakeDrafter) Draft(_ context.Context, _ Intake, docs []string) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	if len(docs) == 0 {
		return "", errors.New("no knowledge available")
	}
	return "Based on our help center, you can update your profile from Account Settings.", nil
}

type MemoryRouter struct {
	mu     sync.Mutex
	Routes []RoutedItem
	Err    error
}

type RoutedItem struct {
	Destination string
	Context     RoutingContext
}

func (r *MemoryRouter) Route(_ context.Context, destination string, routingContext RoutingContext) error {
	if r.Err != nil {
		return r.Err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Routes = append(r.Routes, RoutedItem{Destination: destination, Context: routingContext})
	return nil
}

type MemoryAuditStore struct {
	mu      sync.Mutex
	Records []AuditRecord
	Err     error
}

func (s *MemoryAuditStore) Save(_ context.Context, record AuditRecord) error {
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Records {
		if s.Records[i].DecisionID == record.DecisionID {
			s.Records[i] = record
			return nil
		}
	}
	s.Records = append(s.Records, record)
	return nil
}

func (s *MemoryAuditStore) Get(_ context.Context, decisionID string) (AuditRecord, error) {
	if s.Err != nil {
		return AuditRecord{}, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.Records {
		if record.DecisionID == decisionID {
			return record, nil
		}
	}
	return AuditRecord{}, errors.New("audit record not found")
}

func (s *MemoryAuditStore) CompareAndSetApproval(_ context.Context, decisionID, expectedDraftSHA256 string, status ApprovalStatus, approvedBy string, completedAt time.Time) error {
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Records {
		if s.Records[i].DecisionID == decisionID {
			record := &s.Records[i]
			if (status != ApprovalApproved && status != ApprovalEditedAndApproved) ||
				record.Category != CategoryGeneralQA ||
				record.Action != ActionDraft ||
				record.AuditStatus != AuditPendingApproval ||
				record.ApprovalStatus != ApprovalPending ||
				record.DraftSHA256 != expectedDraftSHA256 {
				return ErrApprovalStateConflict
			}
			record.ApprovalStatus = status
			record.ApprovedBy = approvedBy
			record.AuditStatus = AuditCompleted
			record.CompletedAt = &completedAt
			return nil
		}
	}
	return errors.New("audit record not found")
}
