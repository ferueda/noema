package application

import (
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	PrivacyPolicyVersion = "deterministic-privacy-v1"

	privacyPrivateKey                = "private-key"
	privacyAuthorization             = "authorization-header"
	privacyProviderToken             = "provider-token"
	privacyJWT                       = "jwt"
	privacyLocalPath                 = "local-path"
	privacyURLCredentials            = "url-credentials"
	privacyLocalOrPrivateHost        = "local-or-private-host"
	privacyLocalPathPlaceholder      = "<redacted:local-path>"
	privacyURLCredentialsPlaceholder = "<redacted:url-credentials>"
	privacyPrivateHostPlaceholder    = "<redacted:local-or-private-host>"
	privacyPrivateHostSentinel       = "noema-redacted-host.invalid"
)

var (
	privateKeyPattern    = regexp.MustCompile(`(?i)-----BEGIN[ \t]+(?:(?:ENCRYPTED[ \t]+)?(?:RSA[ \t]+|EC[ \t]+|DSA[ \t]+|OPENSSH[ \t]+)?PRIVATE[ \t]+KEY|PGP[ \t]+PRIVATE[ \t]+KEY[ \t]+BLOCK)-----`)
	authorizationPattern = regexp.MustCompile(
		`(?i)(?:^|[^A-Za-z0-9_-])(?:proxy-)?authorization(?:\\?["'])?\s*[:=]\s*(?:\\?["'])?\s*[A-Za-z][A-Za-z0-9+._~-]*(?:[ \t]+[A-Za-z0-9._~+/=-]{4,})?`,
	)
	providerTokenPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])(?:sk-(?:proj-|svcacct-|ant-)?[A-Za-z0-9_-]{20,}|csk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,255}|github_pat_[A-Za-z0-9_]{20,255}|xox[baprs]-[A-Za-z0-9-]{20,}|(?:AKIA|ASIA)[A-Z0-9]{16}|AIza[0-9A-Za-z_-]{35}|sk_(?:live|test)_[A-Za-z0-9]{16,})(?:$|[^A-Za-z0-9_-])`)
	jwtCandidatePattern  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])([A-Za-z0-9_-]{4,})\.([A-Za-z0-9_-]{4,})\.([A-Za-z0-9_-]*)(?:$|[^A-Za-z0-9_-])`)

	quotedAbsolutePathPattern     = regexp.MustCompile(`(?i)(["'\x60])((?:~?/|[A-Z]:[\\/])[^"'\x60\r\n]+)(["'\x60])`)
	posixPathPattern              = regexp.MustCompile(`(^|[\t\r\n >"'(:=\[{\x60])((?:~(?:/[^/\s"'<>),;}\]]+)+|/(?:[^/\s"'<>),;}\]]+)(?:/[^/\s"'<>),;}\]]+)+|/(?:tmp|root)))`)
	windowsPathPattern            = regexp.MustCompile(`(?i)(^|[\t\r\n >"'(=\[{\x60])([A-Z]:[\\/][^\\/\s"'<>),;}\]]+(?:[\\/][^\\/\s"'<>),;}\]]+)*)`)
	urlPattern                    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`)
	urlCredentialCandidatePattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s/@]+(?::[^\s/@]*)?@`)
	ipv4Pattern                   = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	bracketedIPPattern            = regexp.MustCompile(`\[[0-9A-Fa-f:.%]+\]`)
	unbracketedIPv6Pattern        = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?:%[a-z0-9_.-]+)?`)
	loopbackIPv6Pattern           = regexp.MustCompile(`(^|[^0-9A-Fa-f:])::1($|[^0-9A-Fa-f:])`)
	localHostPattern              = regexp.MustCompile(`(?i)\b(?:localhost|[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.(?:localhost|local|internal|lan))(?::[0-9]{1,5})?\b`)
	carrierGradeNATPrefix         = netip.MustParsePrefix("100.64.0.0/10")
)

// PrivacyCategoryCount records only a category and count, never matched text.
type PrivacyCategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// PrivacyReport is safe to persist because it contains no matched values.
type PrivacyReport struct {
	PolicyVersion     string                 `json:"policyVersion"`
	Redactions        []PrivacyCategoryCount `json:"redactions"`
	BlockedCategories []string               `json:"blockedCategories"`
}

// PrivacyViolation identifies protected categories without exposing their values.
type PrivacyViolation struct {
	Categories []string
}

func (violation PrivacyViolation) Error() string {
	return "privacy policy blocked categories: " + strings.Join(violation.Categories, ", ")
}

// PrivacyPolicy is the fixed deterministic V0 policy. Its zero value is ready to use.
type PrivacyPolicy struct{}

func (PrivacyPolicy) Version() string { return PrivacyPolicyVersion }

// Preflight sanitizes one outbound free-text field.
func (policy PrivacyPolicy) Preflight(value string) (string, PrivacyReport, error) {
	values, report, err := policy.PreflightBatch([]string{value})
	if err != nil {
		return "", report, err
	}
	return values[0], report, nil
}

// PreflightBatch lets callers explicitly traverse every outbound free-text field.
// It blocks the complete batch before returning any partially sanitized values.
func (PrivacyPolicy) PreflightBatch(values []string) ([]string, PrivacyReport, error) {
	report := PrivacyReport{PolicyVersion: PrivacyPolicyVersion, Redactions: []PrivacyCategoryCount{}, BlockedCategories: []string{}}
	blocked := make(map[string]bool)
	for _, value := range values {
		for category := range blockingCategories(value) {
			blocked[category] = true
		}
	}
	if len(blocked) > 0 {
		report.BlockedCategories = sortedCategories(blocked)
		return nil, report, PrivacyViolation{Categories: report.BlockedCategories}
	}

	redactionCounts := make(map[string]int)
	sanitized := make([]string, len(values))
	for index, value := range values {
		sanitized[index] = redactProtectedText(value, redactionCounts)
	}
	for _, value := range sanitized {
		if containsUnredactedURLCredentials(value) {
			blocked[privacyURLCredentials] = true
		}
		for category := range blockingCategories(value) {
			blocked[category] = true
		}
		remaining := make(map[string]int)
		if redactProtectedText(value, remaining) != value {
			for category := range remaining {
				blocked[category] = true
			}
		}
	}
	if len(blocked) > 0 {
		report.BlockedCategories = sortedCategories(blocked)
		return nil, report, PrivacyViolation{Categories: report.BlockedCategories}
	}
	report.Redactions = sortedCounts(redactionCounts)
	return sanitized, report, nil
}

// Postflight rejects protected generated text. It never repairs model output.
func (PrivacyPolicy) Postflight(values ...string) (PrivacyReport, error) {
	report := PrivacyReport{PolicyVersion: PrivacyPolicyVersion, Redactions: []PrivacyCategoryCount{}, BlockedCategories: []string{}}
	blocked := make(map[string]bool)
	for _, value := range values {
		for category := range blockingCategories(value) {
			blocked[category] = true
		}
		counts := make(map[string]int)
		redactProtectedText(value, counts)
		for category, count := range counts {
			if count > 0 {
				blocked[category] = true
			}
		}
		if containsUnredactedURLCredentials(value) {
			blocked[privacyURLCredentials] = true
		}
	}
	if len(blocked) == 0 {
		return report, nil
	}
	report.BlockedCategories = sortedCategories(blocked)
	return report, PrivacyViolation{Categories: report.BlockedCategories}
}

func blockingCategories(value string) map[string]bool {
	blocked := make(map[string]bool)
	if privateKeyPattern.MatchString(value) {
		blocked[privacyPrivateKey] = true
	}
	if authorizationPattern.MatchString(value) {
		blocked[privacyAuthorization] = true
	}
	if providerTokenPattern.MatchString(value) {
		blocked[privacyProviderToken] = true
	}
	if containsJWT(value) {
		blocked[privacyJWT] = true
	}
	return blocked
}

func containsJWT(value string) bool {
	for _, match := range jwtCandidatePattern.FindAllStringSubmatch(value, -1) {
		if len(match) != 4 {
			continue
		}
		header, headerOK := decodeJSONObject(match[1])
		_, payloadOK := decodeJSONObject(match[2])
		algorithm, hasAlgorithm := header["alg"].(string)
		if headerOK && payloadOK && hasAlgorithm && strings.TrimSpace(algorithm) != "" {
			return true
		}
	}
	return false
}

func decodeJSONObject(value string) (map[string]any, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(value)
	}
	if err != nil {
		return nil, false
	}
	var object map[string]any
	if err := json.Unmarshal(decoded, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func containsUnredactedURLCredentials(value string) bool {
	for _, candidate := range urlCredentialCandidatePattern.FindAllString(value, -1) {
		schemeEnd := strings.Index(candidate, "://")
		at := strings.LastIndex(candidate, "@")
		if schemeEnd < 0 || at < schemeEnd+3 || candidate[schemeEnd+3:at] != privacyURLCredentialsPlaceholder {
			return true
		}
	}
	return false
}

func redactProtectedText(value string, counts map[string]int) string {
	value = replaceQuotedPaths(value, counts)
	value = replaceCaptured(value, posixPathPattern, privacyLocalPathPlaceholder, counts, privacyLocalPath)
	value = replaceCaptured(value, windowsPathPattern, privacyLocalPathPlaceholder, counts, privacyLocalPath)
	value = redactURLs(value, counts)
	value = replacePrivateIPs(value, counts)
	value = replaceMatches(value, localHostPattern, privacyPrivateHostPlaceholder, counts, privacyLocalOrPrivateHost)
	return value
}

func replaceQuotedPaths(value string, counts map[string]int) string {
	return quotedAbsolutePathPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := quotedAbsolutePathPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		counts[privacyLocalPath]++
		return parts[1] + privacyLocalPathPlaceholder + parts[3]
	})
}

func replaceCaptured(value string, pattern *regexp.Regexp, replacement string, counts map[string]int, category string) string {
	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		counts[category]++
		return parts[1] + replacement
	})
}

func redactURLs(value string, counts map[string]int) string {
	return urlPattern.ReplaceAllStringFunc(value, func(candidate string) string {
		core, suffix := trimURLPunctuation(candidate)
		parsed, err := url.Parse(core)
		if err != nil || parsed.Scheme == "" {
			return candidate
		}
		if strings.EqualFold(parsed.Scheme, "file") {
			counts[privacyLocalPath]++
			return privacyLocalPathPlaceholder + suffix
		}
		if parsed.Host == "" {
			return candidate
		}
		hadCredentials := parsed.User != nil
		if hadCredentials {
			parsed.User = nil
			counts[privacyURLCredentials]++
		}
		if isLocalOrPrivateHost(parsed.Hostname()) {
			parsed.Host = privacyPrivateHostSentinel
			counts[privacyLocalOrPrivateHost]++
		}
		sanitized := parsed.String()
		sanitized = strings.Replace(sanitized, privacyPrivateHostSentinel, privacyPrivateHostPlaceholder, 1)
		if hadCredentials {
			prefix := parsed.Scheme + "://"
			sanitized = strings.Replace(sanitized, prefix, prefix+privacyURLCredentialsPlaceholder+"@", 1)
		}
		return sanitized + suffix
	})
}

func trimURLPunctuation(value string) (string, string) {
	index := len(value)
	for index > 0 && strings.ContainsRune(".,;!?)", rune(value[index-1])) {
		index--
	}
	return value[:index], value[index:]
}

func replacePrivateIPs(value string, counts map[string]int) string {
	value = ipv4Pattern.ReplaceAllStringFunc(value, func(candidate string) string {
		address, err := netip.ParseAddr(candidate)
		if err != nil || !isLocalOrPrivateAddress(address) {
			return candidate
		}
		counts[privacyLocalOrPrivateHost]++
		return privacyPrivateHostPlaceholder
	})
	value = bracketedIPPattern.ReplaceAllStringFunc(value, func(candidate string) string {
		address, err := netip.ParseAddr(strings.Trim(candidate, "[]"))
		if err != nil || !isLocalOrPrivateAddress(address) {
			return candidate
		}
		counts[privacyLocalOrPrivateHost]++
		return privacyPrivateHostPlaceholder
	})
	value = unbracketedIPv6Pattern.ReplaceAllStringFunc(value, func(candidate string) string {
		address, err := netip.ParseAddr(candidate)
		if err != nil || !isLocalOrPrivateAddress(address) {
			return candidate
		}
		counts[privacyLocalOrPrivateHost]++
		return privacyPrivateHostPlaceholder
	})
	return loopbackIPv6Pattern.ReplaceAllStringFunc(value, func(candidate string) string {
		trimmed := strings.TrimFunc(candidate, func(character rune) bool {
			return !strings.ContainsRune("0123456789abcdefABCDEF:", character)
		})
		if trimmed != "::1" {
			return candidate
		}
		counts[privacyLocalOrPrivateHost]++
		return strings.Replace(candidate, "::1", privacyPrivateHostPlaceholder, 1)
	})
}

func isLocalOrPrivateHost(value string) bool {
	host := strings.TrimSuffix(strings.ToLower(value), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") {
		return true
	}
	if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	address, err := netip.ParseAddr(host)
	return err == nil && isLocalOrPrivateAddress(address)
}

func isLocalOrPrivateAddress(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified() ||
		carrierGradeNATPrefix.Contains(address)
}

func replaceMatches(value string, pattern *regexp.Regexp, replacement string, counts map[string]int, category string) string {
	return pattern.ReplaceAllStringFunc(value, func(string) string {
		counts[category]++
		return replacement
	})
}

func sortedCategories(categories map[string]bool) []string {
	result := make([]string, 0, len(categories))
	for category := range categories {
		result = append(result, category)
	}
	sort.Strings(result)
	return result
}

func sortedCounts(counts map[string]int) []PrivacyCategoryCount {
	categories := make([]string, 0, len(counts))
	for category, count := range counts {
		if count > 0 {
			categories = append(categories, category)
		}
	}
	sort.Strings(categories)
	result := make([]PrivacyCategoryCount, 0, len(categories))
	for _, category := range categories {
		result = append(result, PrivacyCategoryCount{Category: category, Count: counts[category]})
	}
	return result
}
