package triage

type StaticPolicyRegistry struct {
	version  string
	policies map[Category]CategoryPolicy
}

func NewStaticPolicyRegistry() *StaticPolicyRegistry {
	return &StaticPolicyRegistry{
		version: "v1",
		policies: map[Category]CategoryPolicy{
			CategoryGeneralQA: {
				Category:          CategoryGeneralQA,
				Action:            ActionDraft,
				DraftAllowed:      true,
				MinimumConfidence: 0.85,
				ContextMode:       ContextNone,
			},
			CategoryProductFeedback: {
				Category:          CategoryProductFeedback,
				Action:            ActionRoute,
				Destination:       "product",
				DraftAllowed:      false,
				MinimumConfidence: 0.70,
				ContextMode:       ContextStructuredSummary,
			},
			CategoryCompliance: {
				Category:          CategoryCompliance,
				Action:            ActionRoute,
				Destination:       "compliance_legal",
				DraftAllowed:      false,
				MinimumConfidence: 0.0,
				ContextMode:       ContextRestrictedSummary,
			},
		},
	}
}

func (r *StaticPolicyRegistry) Get(category Category) (CategoryPolicy, bool) {
	policy, ok := r.policies[category]
	return policy, ok
}

func (r *StaticPolicyRegistry) Version() string { return r.version }
