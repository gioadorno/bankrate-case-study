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

## Test-first sequence

1. Add release tests for mutated unchanged approval, replay, server time, and edited-response safety; run them RED.
2. Implement authoritative hash-bound atomic approval; rerun focused release tests GREEN.
3. Add routing tests for formatted PII and malicious allowlisted metadata; run RED.
4. Implement the shared detector/redactor and per-field metadata validation; rerun focused privacy tests GREEN.
5. Add malformed classifier confidence/reason and malformed Compliance policy tests; run RED.
6. Add trust-boundary validation and category-level Compliance invariant; rerun focused tests GREEN.
7. Run exact Go 1.23.2 tests, race tests, vet, formatting, build, and an independent read-only review.

## Deliberate deferrals

- Production authentication and authorization; reviewer identity is supplied by an authenticated upstream care workflow.
- Database transactions, durable decision storage, transactional outbox, and existing member-send integration.
- Production DLP/entity recognition and model evaluation; this prototype implements reviewed deterministic patterns and explicit schema boundaries.
- Queueing, retries, circuit breakers, load testing, and per-provider production adapters.

## Live-change readiness

The hardening stays at existing seams: `AuditStore` for approval state, `Sanitizer`/safety helpers for PII, classifier normalization before policy lookup, and `validateDecision` for final invariants. A reviewer can therefore request a new sensitive pattern, metadata rule, category threshold, or policy mutation and observe a focused test-first change without rewriting `Service.Triage`.
