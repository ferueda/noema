# Tighten one-pass semantic claim generation

- Status: completed

## Goal

Use the smallest credible correction for the first reviewed semantic baseline
before adding a second model stage. The baseline completed all 12 corpus
requests, but five candidate batches failed existing local admission rules and
one admitted claim reversed the relationship expressed by its evidence. Three
other admitted claims were only partly supported.

Replace the dense V8 instruction paragraph with a clearer V9 generation
contract and an explicit pre-output self-check. Preserve the current schema,
privacy boundary, deterministic facts, strict atomic admission, processing
lineage, model route, and evaluation corpus. The change succeeds when the
production request carries the new versioned instructions, all offline tests
pass, and Noema is ready for one separately approved comparison run against the
unchanged corpus. Offline tests cannot claim that model quality improved.

## Changes

1. `internal/application/semantic_analysis.go:SemanticPromptVersion` and
   `semanticInstructions` — bump the prompt identity to
   `semantic-claims-v9` and make the existing rules easier for the model to
   follow:
   - treat requests, suggestions, and narrative assertions as evidence that
     something was requested, proposed, or asserted, not as proof that a
     technical state exists;
   - prefer an empty claim set for conversational noise or when the only
     material is an untested suggestion;
   - name `exit-code` and `test-result` as the only V0 result facts that can
     establish an observed failed-attempt or verification outcome;
   - require `outcome: null` for every other claim type and omission of a
     result-bearing claim when a matching result fact is absent;
   - preserve the subject, action, and object expressed by cited evidence
     rather than reversing or inventing their relationship;
   - keep statement, subject, and scope free of the existing forbidden actor
     words, with `actor` and `origin` remaining null;
   - require a lesson derived from a sequence to cite the material failed
     check, change, and later verification and to state a reusable lesson,
     otherwise omit it; and
   - run a concise final checklist over every candidate and omit any candidate
     that violates a rule. Empty output remains successful.

   Keep this as one provider-neutral generation call. Do not add hidden
   provider instructions, a second model call, or a new schema field.

2. `internal/application/semantic_analysis_test.go` and
   `internal/application/semantic_conformance_test.go` — assert the V9 identity
   and the load-bearing instruction rules through the existing production
   request seams. Keep tests offline and avoid asserting the complete prompt
   as one fragile string. Existing claim-admission tests remain the proof that
   invalid outcomes, actor prose, and evidence references still fail locally.

3. `README.md`, `docs/architecture.md`, `docs/roadmap.md`,
   `docs/project-intent.md`, and the active Milestone 2 plan — record that V9 is
   a one-pass correction chosen at the verification checkpoint, not proof that
   the checkpoint is closed. Keep the second verification pass as the next
   option only if the comparison run still admits unsupported or contradicted
   claims.

## Verify

- Run `go test ./internal/application ./cmd/noema-semantic-eval`.
- Run `make check`.
- After implementation, obtain fresh approval for exactly one 12-case
  comparison run against the unchanged digest-pinned corpus. Review every
  admitted claim and case criterion and compare the report with the recorded
  V8 baseline without inventing a release threshold.

## Boundaries

- Do not change the corpus or its digest to improve the score.
- Do not weaken, filter around, or make local batch admission non-atomic.
- Do not add a verifier schema, second model request, persistence fields,
  events, jobs, summaries, or knowledge units in this change.
- Do not make a live model request during implementation or routine tests.

## Implementation status

- V9 replaces the dense instruction paragraph with explicit evidence,
  relationship, result-fact, lesson, actor-language, and final omission rules.
- The schema, route, admission, persistence, events, and corpus digest are
  unchanged.
- Focused offline tests and `make check` pass.
- No live model request was made during implementation.
- The one separately approved comparison ran the unchanged 12-case corpus
  exactly once on 2026-07-23. Eleven batches passed admission and one
  hypothesis batch failed strict local admission.
- Human review covered all 10 admitted claims. All were supported, 9 were
  useful, and 1 was weak. Eight case criteria passed, 2 were partial, and 2
  failed.
- V9 removed the V8 unsupported and partly supported admitted claims, while
  becoming more conservative: it omitted the expected decision and reusable
  lesson. This closes the verifier checkpoint without adding a second model
  call. The recall misses remain visible for evidence-driven follow-up.
