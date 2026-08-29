package triage

import "time"

type Category string

type Action string

type ContextMode string

type ApprovalStatus string

type AuditStatus string

const (
	CategoryGeneralQA       Category = "general_qa"
	CategoryProductFeedback Category = "product_feedback"
	CategoryCompliance      Category = "compliance"
	CategoryUnknown         Category = "unknown"
)

const (
	ActionDraft    Action = "draft_resolution"
	ActionRoute    Action = "route"
	ActionEscalate Action = "escalate_human"
)

const (
	ContextNone              ContextMode = "none"
	ContextStructuredSummary ContextMode = "structured_summary"
	ContextRestrictedSummary ContextMode = "restricted_summary"
)

const (
	AuditPendingExecution AuditStatus = "pending_execution"
	AuditPendingApproval  AuditStatus = "pending_approval"
	AuditCompleted        AuditStatus = "completed"
	AuditDegraded         AuditStatus = "degraded"
)

const (
	ApprovalPending           ApprovalStatus = "pending"
	ApprovalApproved          ApprovalStatus = "approved"
	ApprovalRejected          ApprovalStatus = "rejected"
	ApprovalEditedAndApproved ApprovalStatus = "edited_and_approved"
	ApprovalNotApplicable     ApprovalStatus = "not_applicable"
)

type Intake struct {
	ID       string            `json:"id"`
	MemberID string            `json:"member_id"`
	Claims   map[string]string `json:"claims,omitempty"`
	Text     string            `json:"text"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Classification struct {
	Category   Category `json:"category"`
	Confidence float64  `json:"confidence"`
	ReasonCode string   `json:"reason_code"`
}

type SafetyAssessment struct {
	ComplianceSensitive bool     `json:"compliance_sensitive"`
	SensitiveDataFound  bool     `json:"sensitive_data_found"`
	ReasonCodes         []string `json:"reason_codes"`
}

type CategoryPolicy struct {
	Category          Category
	Action            Action
	Destination       string
	DraftAllowed      bool
	MinimumConfidence float64
	ContextMode       ContextMode
}

type RoutingContext struct {
	CaseID      string            `json:"case_id"`
	Summary     string            `json:"summary"`
	ReasonCodes []string          `json:"reason_codes"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Decision struct {
	DecisionID            string          `json:"decision_id"`
	IntakeID              string          `json:"intake_id"`
	Category              Category        `json:"category"`
	Action                Action          `json:"action"`
	Confidence            float64         `json:"confidence"`
	DraftResponse         *string         `json:"draft_response,omitempty"`
	RoutingContext        *RoutingContext `json:"routing_context,omitempty"`
	HumanApprovalRequired bool            `json:"human_approval_required"`
	ReasonCodes           []string        `json:"reason_codes"`
	AuditID               string          `json:"audit_id,omitempty"`
}

type Approval struct {
	Status           ApprovalStatus `json:"status"`
	ReviewerID       string         `json:"reviewer_id"` // Authenticated by the upstream care workflow in this prototype.
	ReviewedResponse string         `json:"reviewed_response,omitempty"`
	EditedResponse   *string        `json:"edited_response,omitempty"`
}

type MemberRelease struct {
	DecisionID string    `json:"decision_id"`
	Response   string    `json:"response"`
	ApprovedBy string    `json:"approved_by"`
	ApprovedAt time.Time `json:"approved_at"`
}

type AuditRecord struct {
	DecisionID     string
	IntakeID       string
	Category       Category
	Action         Action
	Confidence     float64
	PolicyVersion  string
	ReasonCodes    []string
	SafetyFlags    []string
	DraftSHA256    string
	ApprovalStatus ApprovalStatus
	AuditStatus    AuditStatus
	ApprovedBy     string
	CreatedAt      time.Time
	CompletedAt    *time.Time
}
