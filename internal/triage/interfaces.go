package triage

import (
	"context"
	"time"
)

type SafetyDetector interface {
	Assess(ctx context.Context, intake Intake) (SafetyAssessment, error)
}

type Classifier interface {
	Classify(ctx context.Context, intake Intake) (Classification, error)
}

type KnowledgeBase interface {
	Retrieve(ctx context.Context, query string) ([]string, error)
}

type Drafter interface {
	Draft(ctx context.Context, intake Intake, docs []string) (string, error)
}

type Router interface {
	Route(ctx context.Context, destination string, routingContext RoutingContext) error
}

type AuditStore interface {
	Save(ctx context.Context, record AuditRecord) error
	UpdateApproval(ctx context.Context, decisionID string, status ApprovalStatus, approvedBy string, completedAt time.Time) error
}

type PolicyRegistry interface {
	Get(category Category) (CategoryPolicy, bool)
	Version() string
}

type Sanitizer interface {
	SanitizeText(text string) string
	AllowedMetadata(metadata map[string]string) map[string]string
}
