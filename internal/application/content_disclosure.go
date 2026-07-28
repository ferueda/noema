package application

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ferueda/noema/internal/domain"
)

const (
	disclosurePrivateDetail      = "private-detail"
	disclosurePrivateIdentifier  = "private-identifier"
	disclosureNovelIdentifier    = "novel-identifier"
	privateDetailPlaceholder     = "<private-detail>"
	privateIdentifierPlaceholder = "<private-identifier>"
)

var (
	disclosureLexemePattern       = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}._/@:+-]*`)
	disclosurePhrasePattern       = regexp.MustCompile(`\p{Lu}[\p{L}\p{N}]*(?:[ -]+\p{Lu}[\p{L}\p{N}]*)+`)
	disclosureAbsolutePathPattern = regexp.MustCompile(
		`(^|[\t\r\n >"'(:=\[{\x60])(/[^/\s"'<>),;}\]]+(?:/[^/\s"'<>),;}\]]+)*)`,
	)
	disclosureIdentifierPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`),
		regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
		regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`),
		regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`),
		regexp.MustCompile(`(?i)\b[0-9a-f]{16,}\b`),
		regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`),
		regexp.MustCompile(`@[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`),
		regexp.MustCompile(`\b[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*\b`),
		regexp.MustCompile(`\b[a-z][A-Za-z0-9]*[A-Z][A-Za-z0-9]*\b`),
		regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][A-Za-z0-9]*)+\b`),
		regexp.MustCompile(`\b[A-Za-z0-9]+_[A-Za-z0-9_]+\b`),
		regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_-]*(?:\.[A-Za-z_][A-Za-z0-9_-]*)+\b`),
		regexp.MustCompile(`\b[A-Z][A-Z0-9]{2,}\b`),
	}
)

// ContentDisclosurePolicyV1 is compiled from one exact private input and its
// generalized outbound form. Protected values remain transient and are never
// exposed through reports or errors.
type ContentDisclosurePolicyV1 struct {
	protected          map[string]string
	protectedPhrases   map[string]struct{}
	allowedIdentifiers map[string]struct{}
	approvedTerms      []string
}

type disclosureReportV1 struct {
	counts map[string]int
}

type disclosureViolationV1 struct {
	categories []string
}

func (violation disclosureViolationV1) Error() string {
	return "content disclosure policy blocked categories: " +
		strings.Join(violation.categories, ", ")
}

func compileContentDisclosurePolicyV1(
	privateValues []string,
	outboundValues []string,
	approvedPublicTerms []string,
) (ContentDisclosurePolicyV1, []string, disclosureReportV1, error) {
	if len(privateValues) != len(outboundValues) {
		return ContentDisclosurePolicyV1{}, nil, disclosureReportV1{},
			errors.New("content disclosure input is invalid")
	}
	policy := ContentDisclosurePolicyV1{
		protected:          map[string]string{},
		protectedPhrases:   map[string]struct{}{},
		allowedIdentifiers: map[string]struct{}{},
		approvedTerms:      append([]string{}, approvedPublicTerms...),
	}
	for _, value := range safeDisclosureVocabulary() {
		policy.allowedIdentifiers[normalizeDisclosureValue(value)] = struct{}{}
	}
	for _, value := range approvedPublicTerms {
		policy.allowedIdentifiers[normalizeDisclosureValue(value)] = struct{}{}
	}

	for _, value := range privateValues {
		allowed := policy.allowedSpans(value)
		for _, location := range disclosurePhrasePattern.FindAllStringIndex(value, -1) {
			if containedInDisclosureSpan(location, allowed) {
				continue
			}
			policy.protectedPhrases[normalizeDisclosurePhrase(
				value[location[0]:location[1]],
			)] = struct{}{}
		}
		for _, location := range disclosureLexemePattern.FindAllStringIndex(value, -1) {
			if containedInDisclosureSpan(location, allowed) {
				continue
			}
			token := value[location[0]:location[1]]
			normalized := normalizeDisclosureValue(token)
			if normalized == "" || isGenericDisclosureValue(normalized) {
				continue
			}
			if _, explicitlyAllowed := policy.allowedIdentifiers[normalized]; explicitlyAllowed {
				continue
			}
			category := disclosurePrivateDetail
			if disclosureIdentifierShaped(token) {
				category = disclosurePrivateIdentifier
			}
			policy.protected[normalized] = category
		}
	}
	for _, value := range outboundValues {
		for _, identifier := range disclosureIdentifiers(value) {
			policy.allowedIdentifiers[normalizeDisclosureValue(identifier)] = struct{}{}
		}
	}

	generalized := make([]string, len(outboundValues))
	counts := map[string]int{}
	for index, value := range outboundValues {
		generalized[index] = policy.generalize(value, counts)
	}
	return policy, generalized, disclosureReportV1{counts: counts}, nil
}

func (policy ContentDisclosurePolicyV1) Postflight(
	values ...string,
) (disclosureReportV1, error) {
	counts := map[string]int{}
	for _, value := range values {
		allowed := policy.allowedSpans(value)
		protectedPhrases := policy.protectedPhraseLocations(value, allowed)
		if len(protectedPhrases) > 0 {
			counts[disclosurePrivateDetail] += len(protectedPhrases)
		}
		for _, location := range disclosureLexemePattern.FindAllStringIndex(value, -1) {
			if containedInAnyLocation(location, protectedPhrases) {
				continue
			}
			if containedInDisclosureSpan(location, allowed) {
				continue
			}
			normalized := normalizeDisclosureValue(value[location[0]:location[1]])
			if category := policy.protected[normalized]; category != "" {
				counts[category]++
			}
		}
		for _, location := range disclosureIdentifierLocations(value) {
			if containedInDisclosureSpan(location, allowed) {
				continue
			}
			normalized := normalizeDisclosureValue(value[location[0]:location[1]])
			if _, permitted := policy.allowedIdentifiers[normalized]; !permitted {
				counts[disclosureNovelIdentifier]++
			}
		}
	}
	if len(counts) == 0 {
		return disclosureReportV1{counts: counts}, nil
	}
	return disclosureReportV1{counts: counts}, disclosureViolationV1{
		categories: sortedDisclosureCategories(counts),
	}
}

func (policy ContentDisclosurePolicyV1) generalize(
	value string,
	counts map[string]int,
) string {
	value = policy.generalizePhrases(value, counts)
	allowed := policy.allowedSpans(value)
	locations := disclosureLexemePattern.FindAllStringIndex(value, -1)
	var result strings.Builder
	start := 0
	for _, location := range locations {
		if containedInDisclosureSpan(location, allowed) {
			continue
		}
		category := policy.protected[normalizeDisclosureValue(value[location[0]:location[1]])]
		if category == "" {
			continue
		}
		result.WriteString(value[start:location[0]])
		if category == disclosurePrivateIdentifier {
			result.WriteString(privateIdentifierPlaceholder)
		} else {
			result.WriteString(privateDetailPlaceholder)
		}
		counts[category]++
		start = location[1]
	}
	if start == 0 {
		return value
	}
	result.WriteString(value[start:])
	return result.String()
}

func (policy ContentDisclosurePolicyV1) generalizePhrases(
	value string,
	counts map[string]int,
) string {
	locations := policy.protectedPhraseLocations(value, policy.allowedSpans(value))
	if len(locations) == 0 {
		return value
	}
	var result strings.Builder
	start := 0
	for _, location := range locations {
		result.WriteString(value[start:location[0]])
		result.WriteString(privateDetailPlaceholder)
		counts[disclosurePrivateDetail]++
		start = location[1]
	}
	result.WriteString(value[start:])
	return result.String()
}

func (policy ContentDisclosurePolicyV1) protectedPhraseLocations(
	value string,
	allowed [][2]int,
) [][]int {
	result := make([][]int, 0)
	for _, location := range disclosurePhrasePattern.FindAllStringIndex(value, -1) {
		if containedInDisclosureSpan(location, allowed) {
			continue
		}
		if _, protected := policy.protectedPhrases[normalizeDisclosurePhrase(
			value[location[0]:location[1]],
		)]; protected {
			result = append(result, location)
		}
	}
	return result
}

func (policy ContentDisclosurePolicyV1) allowedSpans(value string) [][2]int {
	terms := append([]string{}, policy.approvedTerms...)
	terms = append(terms, safeDisclosureVocabulary()...)
	spans := make([][2]int, 0)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		spans = append(spans, equalFoldDisclosureSpans(value, term)...)
	}
	return spans
}

func equalFoldDisclosureSpans(value string, term string) [][2]int {
	valueRunes := []rune(value)
	termRunes := []rune(term)
	if len(termRunes) == 0 || len(termRunes) > len(valueRunes) {
		return nil
	}
	byteOffsets := make([]int, len(valueRunes)+1)
	runeIndex := 0
	for byteIndex := range value {
		byteOffsets[runeIndex] = byteIndex
		runeIndex++
	}
	byteOffsets[len(valueRunes)] = len(value)

	result := make([][2]int, 0)
	for start := 0; start+len(termRunes) <= len(valueRunes); start++ {
		end := start + len(termRunes)
		if start > 0 &&
			(unicode.IsLetter(valueRunes[start-1]) || unicode.IsDigit(valueRunes[start-1])) {
			continue
		}
		if end < len(valueRunes) &&
			(unicode.IsLetter(valueRunes[end]) || unicode.IsDigit(valueRunes[end])) {
			continue
		}
		if strings.EqualFold(string(valueRunes[start:end]), term) {
			result = append(result, [2]int{byteOffsets[start], byteOffsets[end]})
		}
	}
	return result
}

func containedInDisclosureSpan(location []int, spans [][2]int) bool {
	for _, span := range spans {
		if location[0] >= span[0] && location[1] <= span[1] {
			return true
		}
	}
	return false
}

func containedInAnyLocation(location []int, containers [][]int) bool {
	for _, container := range containers {
		if location[0] >= container[0] && location[1] <= container[1] {
			return true
		}
	}
	return false
}

func disclosureIdentifierShaped(value string) bool {
	for _, pattern := range disclosureIdentifierPatterns {
		if pattern.FindStringIndex(value) != nil {
			return true
		}
	}
	return false
}

func disclosureIdentifiers(value string) []string {
	locations := disclosureIdentifierLocations(value)
	result := make([]string, 0, len(locations))
	for _, location := range locations {
		result = append(result, value[location[0]:location[1]])
	}
	return result
}

func disclosureIdentifierLocations(value string) [][]int {
	unique := map[[2]int]struct{}{}
	for _, pattern := range disclosureIdentifierPatterns {
		for _, location := range pattern.FindAllStringIndex(value, -1) {
			unique[[2]int{location[0], location[1]}] = struct{}{}
		}
	}
	for _, location := range disclosureCapturedLocations(
		value, disclosureAbsolutePathPattern, 2,
	) {
		unique[[2]int{location[0], location[1]}] = struct{}{}
	}
	result := make([][]int, 0, len(unique))
	for location := range unique {
		result = append(result, []int{location[0], location[1]})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left][0] == result[right][0] {
			return result[left][1] > result[right][1]
		}
		return result[left][0] < result[right][0]
	})
	merged := make([][]int, 0, len(result))
	for _, location := range result {
		if len(merged) == 0 ||
			location[0] >= merged[len(merged)-1][1] {
			merged = append(merged, location)
			continue
		}
		if location[1] > merged[len(merged)-1][1] {
			merged[len(merged)-1][1] = location[1]
		}
	}
	return merged
}

func disclosureCapturedLocations(
	value string,
	pattern *regexp.Regexp,
	group int,
) [][]int {
	result := make([][]int, 0)
	for _, location := range pattern.FindAllStringSubmatchIndex(value, -1) {
		offset := group * 2
		if offset+1 >= len(location) || location[offset] < 0 {
			continue
		}
		result = append(result, []int{location[offset], location[offset+1]})
	}
	return result
}

func normalizeDisclosureValue(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "._/@:+-"))
}

func normalizeDisclosurePhrase(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func sortedDisclosureCategories(counts map[string]int) []string {
	result := make([]string, 0, len(counts))
	for category, count := range counts {
		if count > 0 {
			result = append(result, category)
		}
	}
	sort.Strings(result)
	return result
}

func disclosurePolicyStage(
	name string,
	outcome string,
	report disclosureReportV1,
) domain.AgentPolicyStageV1 {
	categories := make([]domain.AgentPolicyCategoryCountV1, 0, len(report.counts))
	for _, category := range sortedDisclosureCategories(report.counts) {
		categories = append(categories, domain.AgentPolicyCategoryCountV1{
			Category: category,
			Count:    report.counts[category],
		})
	}
	return domain.AgentPolicyStageV1{
		Name:          name,
		PolicyVersion: ContentScoutDisclosurePolicyVersion,
		Outcome:       outcome,
		Categories:    categories,
	}
}

func isGenericDisclosureValue(value string) bool {
	switch value {
	case "a", "about", "after", "agent", "agents", "and", "approach",
		"article", "before", "build", "change", "code", "coding", "command",
		"content", "decision", "developer", "developers", "error", "evidence",
		"failed", "failure", "fix", "for", "from", "how", "idea", "in",
		"issue", "lesson", "model", "of", "on", "problem", "project",
		"result", "root", "session", "software", "solution", "test", "tests",
		"that", "the", "thread", "to", "tool", "tools", "use", "using",
		"useful", "verification", "verify", "was", "what", "when", "why",
		"with", "workflow", "worked":
		return true
	default:
		return false
	}
}

func safeDisclosureVocabulary() []string {
	return []string{
		"AI", "API", "CLI", "Codex", "CSS", "Docker", "Git", "GitHub", "Go",
		"HTML", "HTTP", "HTTPS", "JavaScript", "JSON", "JSONL", "LLM", "Linux",
		"Node.js", "npm", "Python", "React", "SQL", "SQLite", "TypeScript",
	}
}
