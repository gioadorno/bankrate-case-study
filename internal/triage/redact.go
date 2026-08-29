package triage

import "regexp"

type BasicSanitizer struct{}

var sensitiveDataPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`),
	regexp.MustCompile(`(?i)\b(?:acct|account)[_-][A-Za-z0-9]{6,}\b`),
	regexp.MustCompile(`(?i)\b(?:account|routing)\s+number\s*(?::|=|#|\bis\b)\s*(?:[A-Za-z]*\d[A-Za-z0-9]*(?:[ -][A-Za-z]*\d[A-Za-z0-9]*){1,3}|[A-Za-z0-9][A-Za-z0-9_-]{3,})\b`),
	regexp.MustCompile(`\b\d{6,}\b`),
}

var (
	localePattern     = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Z]{2})?$`)
	appVersionPattern = regexp.MustCompile(`^v?\d{1,3}\.\d{1,3}\.\d{1,3}(?:-[0-9A-Za-z.-]{1,32})?(?:\+[0-9A-Za-z.-]{1,32})?$`)
	allowedChannels   = map[string]struct{}{
		"android":    {},
		"chat":       {},
		"email":      {},
		"ios":        {},
		"mobile_web": {},
		"phone":      {},
		"web":        {},
	}
)

func containsSensitiveData(text string) bool {
	for _, pattern := range sensitiveDataPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func redactSensitiveData(text string) string {
	for _, pattern := range sensitiveDataPatterns {
		text = pattern.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

func (BasicSanitizer) SanitizeText(text string) string {
	return redactSensitiveData(text)
}

func (BasicSanitizer) AllowedMetadata(metadata map[string]string) map[string]string {
	out := make(map[string]string)
	for key, value := range metadata {
		if validMetadataValue(key, value) {
			out[key] = value
		}
	}
	return out
}

func validMetadataValue(key, value string) bool {
	if containsSensitiveData(value) {
		return false
	}

	switch key {
	case "channel":
		_, ok := allowedChannels[value]
		return ok
	case "locale":
		return localePattern.MatchString(value)
	case "app_version":
		return len(value) <= 40 && appVersionPattern.MatchString(value)
	default:
		return false
	}
}
