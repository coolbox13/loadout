// Package secrets finds credentials in instruction files and redacts them
// from JSON settings. It deliberately covers what gitleaks' default rules
// missed in practice: passwords inside connection URLs and
// "password: value" prose.
package secrets

import (
	"math"
	"regexp"
	"strings"
)

// Rule identifiers, stable for allowlists and JSON output.
const (
	RuleURLPassword = "url-password"
	RuleTokenPrefix = "token-prefix"
	RuleAssignment  = "secret-assignment"
)

// Redacted replaces secret values in snapshots.
const Redacted = "<REDACTED>"

// Hit is one suspected secret. The value itself is never returned.
type Hit struct {
	Rule   string
	Line   int
	Masked string
}

// closers trail values that end a code construct, e.g. {"k": api_key}.
const closers = "})]>"

// minAssignmentLen is the shortest value treated as a possible secret.
const minAssignmentLen = 8

// minAssignmentEntropy filters words like "changeme" from real values;
// measured in bits per character.
const minAssignmentEntropy = 3.0

var (
	urlPasswordRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]{1,20}://[^\s:/@'"]{0,64}:([^\s@'"]{3,128})@[A-Za-z0-9.\-]+`)
	tokenPrefixRe = regexp.MustCompile(`\b(sk-(?:ant-|proj-)?[A-Za-z0-9_\-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_\-]{16,}|xox[abp]-[A-Za-z0-9\-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_\-]{30,}|sqp_[0-9a-f]{20,})`)
	assignmentRe  = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)\b["']?\s*[:=]\s*["']?([^\s"'` + "`" + `,;]{8,})`)
	// codeExprRe matches values that are code, e.g. process.env.X or getSecret(name).
	codeExprRe    = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.|\[|\()`)
	placeholderRe = regexp.MustCompile(`(?i)^((string|number|boolean|bool|str|int|bytes|none|null|undefined|optional)\b|\$|<|\{|%|your|example|changeme|placeholder|redacted|password|passwd|pass|secret|postgres|root|admin|test|dummy|x{3,}|\*{3,}|\.{3})`)
)

// Scan reports suspected secrets in text, one per match, with line numbers.
func Scan(text string) []Hit {
	var hits []Hit
	for i, line := range strings.Split(text, "\n") {
		hits = append(hits, scanLine(line, i+1)...)
	}
	return hits
}

func scanLine(line string, n int) []Hit {
	var hits []Hit
	for _, m := range urlPasswordRe.FindAllStringSubmatch(line, -1) {
		if !isPlaceholder(m[1]) {
			hits = append(hits, Hit{Rule: RuleURLPassword, Line: n, Masked: mask(m[1])})
		}
	}
	for _, m := range tokenPrefixRe.FindAllString(line, -1) {
		hits = append(hits, Hit{Rule: RuleTokenPrefix, Line: n, Masked: mask(m)})
	}
	if len(hits) > 0 {
		return hits // an assignment on the same line would double-report
	}
	for _, m := range assignmentRe.FindAllStringSubmatch(line, -1) {
		value := strings.TrimRight(m[2], closers)
		if len(value) < minAssignmentLen {
			continue
		}
		if !isPlaceholder(value) && !codeExprRe.MatchString(value) && entropy(value) >= minAssignmentEntropy && !strings.Contains(value, "/") {
			hits = append(hits, Hit{Rule: RuleAssignment, Line: n, Masked: mask(value)})
		}
	}
	return hits
}

func isPlaceholder(value string) bool {
	lower := strings.ToLower(value)
	return placeholderRe.MatchString(value) || strings.Contains(lower, "xxxx") || strings.Contains(lower, "abc123") || strings.Contains(lower, "123456")
}

func mask(value string) string {
	const visible = 4
	if len(value) <= visible {
		return "…"
	}
	return value[:visible] + "…"
}

func entropy(s string) float64 {
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	var h float64
	n := float64(len([]rune(s)))
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

var (
	secretKeyRe   = regexp.MustCompile(`(?i)(key|token|secret|passw|pwd|credential|bearer|auth)`)
	secretValueRe = regexp.MustCompile(`^(sk-|gh[pousr]_|github_pat_|glpat-|xox[abp]-|AKIA|AIza|sqp_|BSA)`)
	urlUserinfoRe = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.\-]*://[^:/@\s]*:)([^@\s]+)(@)`)
)

// RuleSettingsKey marks a literal value under a secret-looking JSON key.
const RuleSettingsKey = "settings-key"

// minSettingsSecretLen ignores short flags and booleans-as-strings.
const minSettingsSecretLen = 8

// ScanJSON reports literal secrets in decoded settings JSON: values under
// secret-looking keys, and values with known token prefixes. Masked holds
// the key path, never the value.
func ScanJSON(v any) []Hit {
	var hits []Hit
	walkJSON(v, "", "", &hits)
	return hits
}

func walkJSON(v any, key, path string, hits *[]Hit) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			walkJSON(child, k, path+"."+k, hits)
		}
	case []any:
		for _, child := range t {
			walkJSON(child, key, path+"[]", hits)
		}
	case string:
		keyHit := secretKeyRe.MatchString(key) && len(t) >= minSettingsSecretLen && !isPlaceholder(t)
		if keyHit || secretValueRe.MatchString(t) || urlUserinfoRe.MatchString(t) && !isPlaceholder(urlUserinfoRe.FindStringSubmatch(t)[2]) {
			*hits = append(*hits, Hit{Rule: RuleSettingsKey, Masked: strings.TrimPrefix(path, ".")})
		}
	}
}

// RedactJSON returns a copy of a decoded JSON value with secrets replaced:
// values under secret-looking keys, values with known token prefixes, and
// passwords inside URLs. The input is never mutated.
func RedactJSON(v any) any {
	return redact(v, "")
}

func redact(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = redact(child, k)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redact(child, key)
		}
		return out
	case string:
		if secretKeyRe.MatchString(key) || secretValueRe.MatchString(t) {
			return Redacted
		}
		return urlUserinfoRe.ReplaceAllString(t, "${1}"+Redacted+"${3}")
	}
	return v
}
