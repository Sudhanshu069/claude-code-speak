package narrator

import "regexp"

// This file scrubs obviously-secret substrings out of text BEFORE it is sent to
// a cloud narrator (see gemini.go). The narrator prompt already asks the model
// to skip code, but the raw text is transmitted verbatim, so a secret Claude
// happened to echo (an API key in a config value, a token in an error message)
// would otherwise leave the machine. Redaction is a defense-in-depth backstop
// for that egress; it is deliberately conservative (clear secret shapes only) to
// avoid mangling ordinary narration. Local narrators (ollama) never call it —
// nothing leaves the machine there.

const redactedMark = "[REDACTED]"

// tokenPatterns match whole secret-shaped tokens; each match becomes [REDACTED].
var tokenPatterns = []*regexp.Regexp{
	// PEM private-key blocks (multi-line; (?s) lets . cross newlines).
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
	// JWTs: three base64url segments.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	// AWS access key id.
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	// Google API key.
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`),
	// GitHub tokens (ghp_/gho_/ghu_/ghs_/ghr_).
	regexp.MustCompile(`\bgh[pousr]_[0-9A-Za-z]{20,}\b`),
	// OpenAI / Anthropic style secret keys (sk-, sk-ant-...).
	regexp.MustCompile(`\bsk-(?:ant-)?[0-9A-Za-z_\-]{16,}\b`),
	// Slack tokens.
	regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`),
}

// bearerPattern redacts the token after a "Bearer " / "Authorization:" prefix,
// keeping the prefix so the sentence still reads sensibly.
var bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)([0-9A-Za-z._\-]{12,})`)

// assignPattern redacts the VALUE of a secret-named assignment (api_key=…,
// password: …, token = "…"), keeping the key, separator, and any quotes so the
// shape of the sentence survives. RE2 has no backreferences, so the quote groups
// are re-emitted rather than balanced.
var assignPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|passwd|pwd|access[_-]?key)\b(\s*[:=]\s*)("?)([^\s"']{6,})("?)`)

// RedactSecrets replaces secret-shaped substrings with [REDACTED]. It is a pure
// function safe to call on any text; on text with no secrets it returns the
// input unchanged (aside from the allocation the regexps may force).
func RedactSecrets(s string) string {
	for _, re := range tokenPatterns {
		s = re.ReplaceAllString(s, redactedMark)
	}
	s = bearerPattern.ReplaceAllString(s, "${1}"+redactedMark)
	s = assignPattern.ReplaceAllString(s, "${1}${2}${3}"+redactedMark+"${5}")
	return s
}
