package triage

import (
	"context"
	"regexp"
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

	if sensitivePattern.MatchString(intake.Text) || strings.Contains(lower, "account number") || strings.Contains(lower, "routing number") {
		assessment.SensitiveDataFound = true
		assessment.ReasonCodes = append(assessment.ReasonCodes, "sensitive_data_detected")
	}

	return assessment, nil
}

var sensitivePattern = regexp.MustCompile(`\b\d{6,}\b`)
