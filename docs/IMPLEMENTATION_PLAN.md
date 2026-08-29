# Submission Safety Hardening Plan

## Goal and timebox

Complete a bounded hardening pass that closes the two reproduced submission blockers: caller-controlled member release and sensitive-data leakage across routing/audit boundaries. Target one focused implementation session; preserve the existing one-pipeline architecture and deterministic prototype scope.

## Requirement matrix

| Case-study requirement | Current failure | Planned evidence |
|---|---|---|
| Nothing reaches a member without human approval | `ReleaseForMember` trusts a caller-supplied `Decision`, so a mutated draft can be released | A test mutates a triaged draft and proves unchanged approval is denied unless its SHA-256 hash matches authoritative audit state |
| Approval must be auditable and replay-safe | Approval updates are keyed only by decision ID and can be repeated | Tests prove one atomic `pending` → approved/edited-approved transition, server-generated time, and replay rejection |
| Sensitive data must not leak into routing/audit context | Formatted SSNs, emails, card/account values, and malicious allowlisted metadata values survive current sanitization | Table-driven routing tests prove shared redaction plus per-field metadata validation removes every sentinel from route, decision, and audit output |
| Low-confidence output must degrade safely | `NaN` bypasses the `< threshold` comparison | Tests prove `NaN`, infinities, values below zero, and values above one become human escalation |
| Explainability metadata must be safe | Classifier reason strings are copied into decisions, routes, and audits | Tests prove only code-owned classifier reason codes are accepted; arbitrary/provider-derived strings degrade safely |
| Compliance-sensitive cases must never receive a member draft | Final validation depends on safety flags and mutable policy settings | A malformed Compliance drafting policy test proves the effective category itself blocks drafting |
| Safety must fail automatically in CI | Existing tests cover only contiguous numeric data and nominal approval | All reproduced adversarial cases become permanent Go tests run by `go test ./...` |

## Major decisions

1. `ReleaseForMember` accepts a decision ID plus an approval command, then reads authoritative audit state. The audit stores the original draft SHA-256 hash, never the raw draft.
2. Approval uses an expected-hash/status compare-and-set inside `AuditStore`; only `pending` General Q&A drafts can transition, and the service supplies the timestamp. Reviewer identity is assumed authenticated upstream for this prototype.
3. One shared sensitive-data layer detects and redacts formatted SSNs, emails, card/account-like digit patterns, and values explicitly associated with account/routing-number labels. Ordinary uses of the words “account” and “routing” are not sensitive by themselves.
4. `channel`, `locale`, and `app_version` use narrow value validators. Invalid values are omitted rather than copied or echoed.
5. Classifier confidence must be finite and within `[0,1]`. Classifier reason codes are selected from a closed internal set; malformed output degrades to the human path using code-owned reasons.
6. Effective `CategoryCompliance` independently forbids a draft at execution and final validation. Lightweight generated/edited draft inspection is defense-in-depth, not a general content-filtering subsystem.

## Test-first tasks

### Task 1: Authoritative approval and replay safety

**Files:** `internal/triage/interfaces.go`, `internal/triage/types.go`, `internal/triage/release.go`, `internal/triage/fakes.go`, `internal/triage/release_test.go`, and constructor call sites as required.

1. Add focused tests for a mutated unchanged approval, replay, server-generated time, authoritative Compliance state, and unsafe edited content.
2. Run only the release tests and verify each new behavior fails for the expected missing boundary.
3. Add authoritative audit lookup, original-draft SHA-256 binding, and atomic pending-state transition.
4. Rerun release tests, then the complete suite.

### Task 2: Shared PII boundary and safe metadata values

**Files:** `internal/triage/redact.go`, `internal/triage/safety.go`, `internal/triage/service.go`, and `internal/triage/service_test.go`.

1. Add table-driven route tests for formatted SSNs, emails, card/account values, explicit account/routing-number values, and malicious `channel`, `locale`, and `app_version` values.
2. Run the focused privacy tests and verify the sentinels currently reach observable routing output.
3. Implement one shared detector/redactor plus narrow validators for each allowlisted metadata field. Do not treat the words “account” or “routing” alone as sensitive.
4. Inspect generated and edited drafts with the same lightweight boundary, then rerun focused and complete tests.

### Task 3: Classifier validation, Compliance invariant, and submission evidence

**Files:** `internal/triage/service.go`, `internal/triage/policies.go`, `internal/triage/service_test.go`, `README.md`, `RFC.md`, and `AI_LEVERAGE_LOG.md`.

1. Add tests for `NaN`, both infinities, confidence outside `[0,1]`, arbitrary classifier reason data, and a deliberately malformed Compliance drafting policy.
2. Run focused tests and verify invalid confidence can bypass the current threshold and malformed policy can draft.
3. Add classifier normalization with code-owned reason codes and an effective-category Compliance no-draft invariant at execution and final validation.
4. Update documentation with the implemented trust boundaries, authenticated-upstream reviewer assumption, accurate deferrals, and adversarial AI-review narrative while keeping the AI Leverage Log at two paragraphs.
5. Run exact Go 1.23.2 tests, race tests, vet, formatting, build, and independent review.

## Deliberate deferrals

- Production authentication and authorization; reviewer identity is supplied by an authenticated upstream care workflow.
- Database transactions, durable decision storage, transactional outbox, and existing member-send integration.
- Production DLP/entity recognition and model evaluation; this prototype implements reviewed deterministic patterns and explicit schema boundaries.
- Queueing, retries, circuit breakers, load testing, and per-provider production adapters.

## Live-change readiness

The hardening stays at existing seams: `AuditStore` for approval state, `Sanitizer`/safety helpers for PII, classifier normalization before policy lookup, and `validateDecision` for final invariants. A reviewer can therefore request a new sensitive pattern, metadata rule, category threshold, or policy mutation and observe a focused test-first change without rewriting `Service.Triage`.
