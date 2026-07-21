package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ferueda/noema/internal/domain"
	noemaevidence "github.com/ferueda/noema/internal/evidence"
)

const (
	SemanticInputSchemaVersion = 1

	semanticInputDisposition = "untrusted-evidence"
	semanticCoveragePartial  = "partial"

	maxSemanticEntries              = 50
	maxSemanticEvidenceRefs         = 512
	maxSemanticFacts                = 256
	maxSemanticTextValueBytes       = 8 * 1024
	maxSemanticEvidenceSectionBytes = 256 * 1024
	maxSemanticFactSectionBytes     = 128 * 1024
	maxSemanticInputBytes           = 512 * 1024
)

var (
	ErrSemanticInputInvalid  = errors.New("semantic-input-invalid")
	ErrSemanticInputTooLarge = errors.New("semantic-input-too-large")
)

// EntryBounds selects one inclusive, contiguous range. Both values must be
// supplied together; an empty value means the complete retained snapshot.
type EntryBounds struct {
	First *int
	Last  *int
}

type SemanticTextInput struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	// OriginalUTF8Bytes describes the local source value. A typed privacy
	// placeholder can be longer than the text it replaces.
	OriginalUTF8Bytes int `json:"originalUtf8Bytes"`
	EmittedUTF8Bytes  int `json:"emittedUtf8Bytes"`
}

type SemanticSegmentInput struct {
	EvidenceID       string             `json:"evidenceId"`
	Ordinal          int                `json:"ordinal"`
	Kind             string             `json:"kind"`
	Origin           string             `json:"origin"`
	OriginConfidence string             `json:"originConfidence"`
	Text             *SemanticTextInput `json:"text,omitempty"`
	ContentClass     string             `json:"contentClass,omitempty"`
	SourceType       string             `json:"sourceType,omitempty"`
}

type SemanticEntryInput struct {
	EvidenceID          string                 `json:"evidenceId"`
	Ordinal             int                    `json:"ordinal"`
	Kind                string                 `json:"kind"`
	Actor               string                 `json:"actor"`
	Timestamp           *time.Time             `json:"timestamp,omitempty"`
	RelatedEntryOrdinal *int                   `json:"relatedEntryOrdinal,omitempty"`
	ToolName            string                 `json:"toolName,omitempty"`
	ToolNamespace       string                 `json:"toolNamespace,omitempty"`
	Segments            []SemanticSegmentInput `json:"segments"`
}

type SemanticToolFactInput struct {
	Kind                string `json:"kind"`
	Name                string `json:"name,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	RelatedEntryOrdinal *int   `json:"relatedEntryOrdinal,omitempty"`
}

type SemanticTestFactInput struct {
	Framework string             `json:"framework"`
	Command   *SemanticTextInput `json:"command,omitempty"`
	Passed    *int               `json:"passed,omitempty"`
	Failed    *int               `json:"failed,omitempty"`
	Skipped   *int               `json:"skipped,omitempty"`
}

type SemanticFactValueInput struct {
	Tool     *SemanticToolFactInput `json:"tool,omitempty"`
	Command  *SemanticTextInput     `json:"command,omitempty"`
	Test     *SemanticTestFactInput `json:"test,omitempty"`
	ExitCode *int                   `json:"exitCode,omitempty"`
	Error    *SemanticTextInput     `json:"error,omitempty"`
}

type SemanticFactInput struct {
	ID          string                 `json:"id"`
	Kind        string                 `json:"kind"`
	Outcome     string                 `json:"outcome"`
	Value       SemanticFactValueInput `json:"value"`
	EvidenceIDs []string               `json:"evidenceIds"`
}

type SemanticSelection struct {
	Mode                     string `json:"mode"`
	SelectedEntries          int    `json:"selectedEntries"`
	TotalEntries             int    `json:"totalEntries"`
	FirstOrdinal             *int   `json:"firstOrdinal,omitempty"`
	LastOrdinal              *int   `json:"lastOrdinal,omitempty"`
	OriginalTextUTF8Bytes    int    `json:"originalTextUtf8Bytes"`
	EmittedTextUTF8Bytes     int    `json:"emittedTextUtf8Bytes"`
	TruncatedTextSegments    int    `json:"truncatedTextSegments"`
	TruncatedFactTexts       int    `json:"truncatedFactTexts"`
	CanonicalOmittedSegments int    `json:"canonicalOmittedSegments"`
	ExcludedFactCount        int    `json:"excludedFactCount"`
	Coverage                 string `json:"coverage"`
}

type SemanticInputOmissions struct {
	FactAnalysis domain.AnalysisOmissions `json:"factAnalysis"`
}

// SemanticModelInput is the complete bounded value a generator may receive.
// Source identities and full EvidenceRef values remain in PreparedSemanticInput.
type SemanticModelInput struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Disposition   string                 `json:"disposition"`
	Selection     SemanticSelection      `json:"selection"`
	Entries       []SemanticEntryInput   `json:"entries"`
	Facts         []SemanticFactInput    `json:"facts"`
	Omissions     SemanticInputOmissions `json:"omissions"`
}

// PreparedSemanticInput keeps the exact local records used to validate model
// identifiers. Only ModelInput is eligible to cross the generator boundary.
type PreparedSemanticInput struct {
	ModelInput   SemanticModelInput            `json:"modelInput"`
	EvidenceByID map[string]domain.EvidenceRef `json:"-"`
	FactsByID    map[string]domain.Fact        `json:"-"`
	OrderedFacts []domain.Fact                 `json:"-"`
}

type semanticInputLimits struct {
	entries, evidenceRefs, facts                           int
	textValueBytes, evidenceSectionBytes, factSectionBytes int
	inputBytes                                             int
}

var defaultSemanticInputLimits = semanticInputLimits{
	entries: maxSemanticEntries, evidenceRefs: maxSemanticEvidenceRefs, facts: maxSemanticFacts,
	textValueBytes: maxSemanticTextValueBytes, evidenceSectionBytes: maxSemanticEvidenceSectionBytes,
	factSectionBytes: maxSemanticFactSectionBytes, inputBytes: maxSemanticInputBytes,
}

func BuildSemanticInput(
	analysis domain.FactAnalysis,
	document domain.EvidenceDocument,
	bounds EntryBounds,
) (PreparedSemanticInput, error) {
	return buildSemanticInput(analysis, document, bounds, defaultSemanticInputLimits)
}

func buildSemanticInput(
	analysis domain.FactAnalysis,
	document domain.EvidenceDocument,
	bounds EntryBounds,
	limits semanticInputLimits,
) (PreparedSemanticInput, error) {
	if err := validateSemanticSource(analysis, document); err != nil {
		return PreparedSemanticInput{}, err
	}
	first, last, explicit, err := semanticRange(len(document.Entries), bounds)
	if err != nil {
		return PreparedSemanticInput{}, err
	}
	selectedCount := 0
	if first != nil {
		selectedCount = *last - *first + 1
	}
	if selectedCount > limits.entries {
		return PreparedSemanticInput{}, fmt.Errorf("%w: entry count", ErrSemanticInputTooLarge)
	}

	prepared := PreparedSemanticInput{
		EvidenceByID: make(map[string]domain.EvidenceRef),
		FactsByID:    make(map[string]domain.Fact),
		ModelInput: SemanticModelInput{
			SchemaVersion: SemanticInputSchemaVersion,
			Disposition:   semanticInputDisposition,
			Entries:       make([]SemanticEntryInput, 0, selectedCount),
			Facts:         []SemanticFactInput{},
			Omissions:     SemanticInputOmissions{FactAnalysis: analysis.Run.Omissions},
		},
	}
	selection := SemanticSelection{
		Mode:            "complete",
		SelectedEntries: selectedCount,
		TotalEntries:    len(document.Entries),
		FirstOrdinal:    cloneOptionalInt(first),
		LastOrdinal:     cloneOptionalInt(last),
		Coverage:        domain.CoverageCompleteRetainedSnapshot,
	}
	if explicit {
		selection.Mode = "range"
	}

	if first != nil {
		for ordinal := *first; ordinal <= *last; ordinal++ {
			entry := document.Entries[ordinal]
			entryRef, err := noemaevidence.SessionsReference(document, ordinal, nil)
			if err != nil {
				return PreparedSemanticInput{}, fmt.Errorf("%w: entry evidence", ErrSemanticInputInvalid)
			}
			if err := addSemanticReference(prepared.EvidenceByID, entryRef, limits.evidenceRefs); err != nil {
				return PreparedSemanticInput{}, err
			}
			outboundEntry := SemanticEntryInput{
				EvidenceID: entryRef.ID, Ordinal: entry.Ordinal, Kind: entry.Kind, Actor: entry.Actor,
				Timestamp: entry.Timestamp, RelatedEntryOrdinal: cloneOptionalInt(entry.RelatedEntryOrdinal),
				ToolName: entry.ToolName, ToolNamespace: entry.ToolNamespace,
				Segments: make([]SemanticSegmentInput, 0, len(entry.Content)),
			}
			for segmentIndex := range entry.Content {
				segment := entry.Content[segmentIndex]
				segmentOrdinal := segment.Ordinal
				ref, err := noemaevidence.SessionsReference(document, ordinal, &segmentOrdinal)
				if err != nil {
					return PreparedSemanticInput{}, fmt.Errorf("%w: segment evidence", ErrSemanticInputInvalid)
				}
				if err := addSemanticReference(prepared.EvidenceByID, ref, limits.evidenceRefs); err != nil {
					return PreparedSemanticInput{}, err
				}
				outboundSegment := SemanticSegmentInput{
					EvidenceID: ref.ID, Ordinal: segment.Ordinal, Kind: segment.Kind,
					Origin: segment.Origin, OriginConfidence: segment.OriginConfidence,
					ContentClass: segment.ContentClass, SourceType: segment.SourceType,
				}
				if segment.Kind == "omitted" {
					selection.CanonicalOmittedSegments++
				}
				if segment.Text != nil {
					text, truncated := semanticText(*segment.Text, limits.textValueBytes)
					if truncated && !explicit {
						return PreparedSemanticInput{}, fmt.Errorf("%w: complete snapshot text", ErrSemanticInputTooLarge)
					}
					outboundSegment.Text = &text
					selection.OriginalTextUTF8Bytes += text.OriginalUTF8Bytes
					selection.EmittedTextUTF8Bytes += text.EmittedUTF8Bytes
					if truncated {
						selection.TruncatedTextSegments++
					}
				}
				outboundEntry.Segments = append(outboundEntry.Segments, outboundSegment)
			}
			prepared.ModelInput.Entries = append(prepared.ModelInput.Entries, outboundEntry)
		}
	}

	for _, fact := range analysis.Facts {
		selected := true
		for _, ref := range fact.Evidence {
			if _, ok := prepared.EvidenceByID[ref.ID]; !ok {
				selected = false
				break
			}
		}
		if !selected {
			selection.ExcludedFactCount++
			continue
		}
		if len(prepared.OrderedFacts) >= limits.facts {
			return PreparedSemanticInput{}, fmt.Errorf("%w: fact count", ErrSemanticInputTooLarge)
		}
		outboundFact := semanticFact(fact)
		for _, text := range semanticFactTexts(&outboundFact.Value) {
			if text.Truncated {
				selection.TruncatedFactTexts++
			}
		}
		prepared.OrderedFacts = append(prepared.OrderedFacts, fact)
		prepared.FactsByID[fact.ID] = fact
		prepared.ModelInput.Facts = append(prepared.ModelInput.Facts, outboundFact)
	}

	if selectedCount != len(document.Entries) || selection.TruncatedTextSegments > 0 || selection.TruncatedFactTexts > 0 {
		selection.Coverage = semanticCoveragePartial
	}
	prepared.ModelInput.Selection = selection
	if err := validateSemanticEncodedSize(prepared.ModelInput, limits); err != nil {
		return PreparedSemanticInput{}, err
	}
	return prepared, nil
}

func validateSemanticSource(analysis domain.FactAnalysis, document domain.EvidenceDocument) error {
	if analysis.Run.Stage != domain.AnalysisStageFacts || analysis.Run.Status != domain.AnalysisCompleted ||
		analysis.Run.Revision == nil || analysis.Run.Selection == nil {
		return fmt.Errorf("%w: fact analysis", ErrSemanticInputInvalid)
	}
	if analysis.Run.RequestedSourceIdentity != document.Revision.CanonicalID ||
		analysis.Run.Revision.Identity() != document.Revision.Identity() {
		return ErrSourceRevisionUnavailable
	}
	if len(analysis.Run.FactIDs) != len(analysis.Facts) {
		return fmt.Errorf("%w: fact order", ErrSemanticInputInvalid)
	}
	seenFacts := make(map[string]bool, len(analysis.Facts))
	for index, fact := range analysis.Facts {
		if fact.ID == "" || fact.ID != analysis.Run.FactIDs[index] || fact.AnalysisRunID != analysis.Run.ID ||
			seenFacts[fact.ID] || len(fact.Evidence) == 0 {
			return fmt.Errorf("%w: fact identity", ErrSemanticInputInvalid)
		}
		seenFacts[fact.ID] = true
		for _, ref := range fact.Evidence {
			if err := noemaevidence.ValidateSessionsReference(document, ref); err != nil {
				return fmt.Errorf("%w: fact evidence", ErrSemanticInputInvalid)
			}
		}
	}
	for index, entry := range document.Entries {
		if entry.Ordinal != index {
			return fmt.Errorf("%w: entry order", ErrSemanticInputInvalid)
		}
		for segmentIndex, segment := range entry.Content {
			if segment.Ordinal != segmentIndex {
				return fmt.Errorf("%w: segment order", ErrSemanticInputInvalid)
			}
		}
	}
	return nil
}

func semanticRange(total int, bounds EntryBounds) (first, last *int, explicit bool, err error) {
	if (bounds.First == nil) != (bounds.Last == nil) {
		return nil, nil, false, fmt.Errorf("%w: entry bounds must be paired", ErrSemanticInputInvalid)
	}
	if bounds.First == nil {
		if total == 0 {
			return nil, nil, false, nil
		}
		firstValue, lastValue := 0, total-1
		return &firstValue, &lastValue, false, nil
	}
	if *bounds.First < 0 || *bounds.Last < *bounds.First || *bounds.Last >= total {
		return nil, nil, true, fmt.Errorf("%w: entry bounds", ErrSemanticInputInvalid)
	}
	return cloneOptionalInt(bounds.First), cloneOptionalInt(bounds.Last), true, nil
}

func addSemanticReference(refs map[string]domain.EvidenceRef, ref domain.EvidenceRef, limit int) error {
	if existing, ok := refs[ref.ID]; ok {
		if !sameEvidenceReference(existing, ref) {
			return fmt.Errorf("%w: evidence identity collision", ErrSemanticInputInvalid)
		}
		return nil
	}
	if len(refs) >= limit {
		return fmt.Errorf("%w: evidence count", ErrSemanticInputTooLarge)
	}
	refs[ref.ID] = ref
	return nil
}

func semanticText(text domain.SelectedText, limit int) (SemanticTextInput, bool) {
	value := text.Text
	if len([]byte(value)) > limit {
		value = truncateUTF8(value, limit)
	}
	emitted := len([]byte(value))
	truncated := emitted < text.OriginalUTF8Bytes
	return SemanticTextInput{
		Text: value, Truncated: truncated, OriginalUTF8Bytes: text.OriginalUTF8Bytes, EmittedUTF8Bytes: emitted,
	}, truncated
}

func semanticFact(fact domain.Fact) SemanticFactInput {
	evidenceIDs := make([]string, len(fact.Evidence))
	for index := range fact.Evidence {
		evidenceIDs[index] = fact.Evidence[index].ID
	}
	value := SemanticFactValueInput{ExitCode: fact.Value.ExitCode}
	if fact.Value.Tool != nil {
		value.Tool = &SemanticToolFactInput{
			Kind: fact.Value.Tool.Kind, Name: fact.Value.Tool.Name, Namespace: fact.Value.Tool.Namespace,
			RelatedEntryOrdinal: cloneOptionalInt(fact.Value.Tool.RelatedEntryOrdinal),
		}
	}
	value.Command = semanticSelectedText(fact.Value.Command)
	value.Error = semanticSelectedText(fact.Value.Error)
	if fact.Value.Test != nil {
		value.Test = &SemanticTestFactInput{
			Framework: fact.Value.Test.Framework, Command: semanticSelectedText(fact.Value.Test.Command),
			Passed: fact.Value.Test.Passed, Failed: fact.Value.Test.Failed, Skipped: fact.Value.Test.Skipped,
		}
	}
	return SemanticFactInput{ID: fact.ID, Kind: fact.Kind, Outcome: fact.Outcome, Value: value, EvidenceIDs: evidenceIDs}
}

func semanticSelectedText(text *domain.SelectedText) *SemanticTextInput {
	if text == nil {
		return nil
	}
	return &SemanticTextInput{
		Text: text.Text, Truncated: text.Truncated, OriginalUTF8Bytes: text.OriginalUTF8Bytes,
		EmittedUTF8Bytes: text.EmittedUTF8Bytes,
	}
}

func semanticFactTexts(value *SemanticFactValueInput) []*SemanticTextInput {
	texts := make([]*SemanticTextInput, 0, 3)
	if value.Command != nil {
		texts = append(texts, value.Command)
	}
	if value.Error != nil {
		texts = append(texts, value.Error)
	}
	if value.Test != nil && value.Test.Command != nil {
		texts = append(texts, value.Test.Command)
	}
	return texts
}

func validateSemanticEncodedSize(input SemanticModelInput, limits semanticInputLimits) error {
	entries, err := json.Marshal(input.Entries)
	if err != nil {
		return fmt.Errorf("%w: encode evidence", ErrSemanticInputInvalid)
	}
	if len(entries) > limits.evidenceSectionBytes {
		return fmt.Errorf("%w: evidence section", ErrSemanticInputTooLarge)
	}
	facts, err := json.Marshal(input.Facts)
	if err != nil {
		return fmt.Errorf("%w: encode facts", ErrSemanticInputInvalid)
	}
	if len(facts) > limits.factSectionBytes {
		return fmt.Errorf("%w: fact section", ErrSemanticInputTooLarge)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("%w: encode input", ErrSemanticInputInvalid)
	}
	if len(encoded) > limits.inputBytes {
		return fmt.Errorf("%w: complete input", ErrSemanticInputTooLarge)
	}
	return nil
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameEvidenceReference(left, right domain.EvidenceRef) bool {
	leftSegment, rightSegment := -1, -1
	if left.SegmentOrdinal != nil {
		leftSegment = *left.SegmentOrdinal
	}
	if right.SegmentOrdinal != nil {
		rightSegment = *right.SegmentOrdinal
	}
	leftRelated, rightRelated := -1, -1
	if left.RelatedEntryOrdinal != nil {
		leftRelated = *left.RelatedEntryOrdinal
	}
	if right.RelatedEntryOrdinal != nil {
		rightRelated = *right.RelatedEntryOrdinal
	}
	left.SegmentOrdinal, right.SegmentOrdinal = nil, nil
	left.RelatedEntryOrdinal, right.RelatedEntryOrdinal = nil, nil
	return left == right && leftSegment == rightSegment && leftRelated == rightRelated
}
