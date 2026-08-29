package triage

import (
	"context"
	"strings"
)

type RuleSafetyDetector struct{}

func (RuleSafetyDetector) Assess(_ context.Context, intake Intake) (SafetyAssessment, error) {
	lower := strings.ToLower(intake.Text)
	assessment := SafetyAssessment{}

	complianceTerms := []string{
		"illegal", "law", "lawsuit", "regulator", "regulatory", "discrimination", "consumer protection", "violation",
	}
	for _, term := range complianceTerms {
		if strings.Contains(lower, term) {
			assessment.ComplianceSensitive = true
			assessment.ReasonCodes = append(assessment.ReasonCodes, "compliance_signal_detected")
			break
		}
	}

	if containsSensitiveData(intake.Text) {
		assessment.SensitiveDataFound = true
		assessment.ReasonCodes = append(assessment.ReasonCodes, "sensitive_data_detected")
	}

	return assessment, nil
}
