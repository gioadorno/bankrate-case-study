package triage

import "regexp"

type BasicSanitizer struct{}

var longNumberPattern = regexp.MustCompile(`\b\d{6,}\b`)

func (BasicSanitizer) SanitizeText(text string) string {
	return longNumberPattern.ReplaceAllString(text, "[REDACTED]")
}

func (BasicSanitizer) AllowedMetadata(metadata map[string]string) map[string]string {
	allowedKeys := map[string]struct{}{
		"channel":     {},
		"locale":      {},
		"app_version": {},
	}

	out := make(map[string]string)
	for key, value := range metadata {
		if _, ok := allowedKeys[key]; ok {
			out[key] = value
		}
	}
	return out
}
