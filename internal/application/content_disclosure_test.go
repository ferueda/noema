package application

import (
	"strings"
	"testing"
)

func TestContentDisclosurePolicyGeneralizesProtectedUnicodeInputWithoutBreakingOffsets(
	t *testing.T,
) {
	private := []string{"İstanbul and SecretProject uses Go"}
	policy, generalized, report, err := compileContentDisclosurePolicyV1(
		private,
		append([]string{}, private...),
		[]string{"İstanbul"},
	)
	if err != nil {
		t.Fatalf("compile disclosure policy: %v", err)
	}
	if len(generalized) != 1 ||
		!strings.Contains(generalized[0], "İstanbul") ||
		strings.Contains(generalized[0], "SecretProject") ||
		!strings.Contains(generalized[0], privateIdentifierPlaceholder) {
		t.Fatalf("generalized input = %q", generalized)
	}
	if report.counts[disclosurePrivateIdentifier] != 1 {
		t.Fatalf("generalization report = %#v", report)
	}
	if _, err := policy.Postflight("A lesson from İstanbul and Go"); err != nil {
		t.Fatalf("approved Unicode term was blocked: %v", err)
	}
	if _, err := policy.Postflight("SecretProject should remain private"); err == nil {
		t.Fatal("protected Unicode-adjacent identifier was accepted")
	}
}

func TestContentDisclosurePolicyRejectsNovelIdentifiersButAllowsOrdinaryProse(
	t *testing.T,
) {
	policy, _, _, err := compileContentDisclosurePolicyV1(
		[]string{"Tests failed before the fix"},
		[]string{"Tests failed before the fix"},
		nil,
	)
	if err != nil {
		t.Fatalf("compile disclosure policy: %v", err)
	}
	if _, err := policy.Postflight(
		"A useful lesson about tests that fail before a fix",
	); err != nil {
		t.Fatalf("ordinary prose was blocked: %v", err)
	}
	report, err := policy.Postflight("The hidden issue was ABC-123")
	if err == nil || report.counts[disclosureNovelIdentifier] != 1 {
		t.Fatalf("novel identifier result = %#v, %v", report, err)
	}
}

func TestContentDisclosurePolicyDoesNotLetSafePrefixExemptPrivateIdentifier(
	t *testing.T,
) {
	private := []string{"The repository is Go/PrivateRepo"}
	policy, generalized, _, err := compileContentDisclosurePolicyV1(
		private, append([]string{}, private...), nil,
	)
	if err != nil {
		t.Fatalf("compile disclosure policy: %v", err)
	}
	if strings.Contains(generalized[0], "Go/PrivateRepo") {
		t.Fatalf("safe prefix exempted private identifier: %q", generalized[0])
	}
	if _, err := policy.Postflight("Look at Go/PrivateRepo"); err == nil {
		t.Fatal("safe prefix exempted protected output")
	}
}

func TestContentDisclosurePolicyProtectsMultiTokenSourcePhrase(t *testing.T) {
	private := []string{"The internal name is Project Code"}
	policy, generalized, _, err := compileContentDisclosurePolicyV1(
		private, append([]string{}, private...), nil,
	)
	if err != nil {
		t.Fatalf("compile disclosure policy: %v", err)
	}
	if strings.Contains(generalized[0], "Project Code") {
		t.Fatalf("private phrase was not generalized: %q", generalized[0])
	}
	if _, err := policy.Postflight("A lesson from Project Code"); err == nil {
		t.Fatal("private phrase was accepted")
	}

	approved, approvedInput, _, err := compileContentDisclosurePolicyV1(
		private, append([]string{}, private...), []string{"Project Code"},
	)
	if err != nil {
		t.Fatalf("compile approved disclosure policy: %v", err)
	}
	if !strings.Contains(approvedInput[0], "Project Code") {
		t.Fatalf("approved phrase was generalized: %q", approvedInput[0])
	}
	if _, err := approved.Postflight("A lesson from Project Code"); err != nil {
		t.Fatalf("approved phrase was blocked: %v", err)
	}
}
