# RFC: Support Triage & Resolution Agent

## 1. Executive summary

The proposed system augments the existing Customer Support workflow rather than replacing it. The existing ticket/case system remains the source of truth and the manual process remains the safety net. A shared triage pipeline classifies each intake, resolves category policy, applies independent safety constraints, then either drafts a response for human approval, routes a sanitized context package, or escalates to the existing human queue.

The core safety principle is **fail closed toward humans**. Compliance-sensitive signals can never produce a member-facing draft, low-confidence or dependency-failure cases return to the manual path, and a final egress guard validates safety invariants before a decision is allowed to progress.

## 2. Goals

- Support general Q&A, product feedback, and compliance through one coherent pipeline.
- Keep category-specific policy separate from shared pipeline code.
- Make compliance safety structural rather than prompt-dependent.
- Preserve the current manual workflow throughout rollout.
- Provide explainable, auditable decisions without copying raw sensitive intake data into logs or routing payloads.
- Design toward bursty hundreds-to-thousands/day volume and the long-term ~80% AI-resolution target.

## 3. Non-goals

- General-purpose omnichannel support platform.
- Production authentication; identity is assumed authenticated upstream.
- Production LLM, vector database, member-profile, ticketing, or cloud integrations.
- Automated member sends in v1.
- Replacing the existing ticket/case system.

## 4. Assumptions

1. Member identity/claims are authenticated before the request reaches triage.
2. The existing case is persisted before triage runs and remains the raw-data system of record.
3. Triage processes raw text transiently but does not create a second raw-intake store.
4. Every member-facing draft requires human approval in v1.
5. Explicit or uncertain compliance signals fail closed to Compliance/Legal or a human path.
6. Low-confidence decisions fail closed to humans.
7. Adding a category is configuration-only when it uses an existing action primitive; a truly new action may require a new executor.
8. The prototype uses deterministic/fake dependencies to demonstrate system boundaries rather than model sophistication.
9. Audit/routing records exclude raw intake text and sensitive member/account data.
10. Audit metadata retention is assumed to be 365 days for this exercise and should be replaced by the real Legal/Compliance retention policy.

## 5. Proposed architecture

### Production shape

```text
Member intake
    |
    v
Existing ticket/case system  <-- raw source of truth; manual path survives AI failure
    |
    v
Durable triage job / queue
    |
    v
Triage worker
    |
    +--> safety assessment
    +--> classifier
    +--> category policy
    +--> action executor
    +--> final egress safety gate
    +--> sanitized audit record
    |
    +--> draft -> human approval -> release gate -> existing send workflow
    +--> sanitized route -> Product / Compliance-Legal
    +--> degraded/low confidence -> existing human queue
```

The prototype exposes a synchronous `POST /v1/triage` for inspectability. Production would decouple case persistence from triage with a durable queue because intake acceptance does not need to wait on LLM, knowledge-base, profile, or routing dependencies.

## 6. One pipeline, many categories

Classification returns a category and confidence; it does not directly execute business behavior. A policy registry maps a category to one of three action primitives: `draft_resolution`, `route`, or `escalate_human`.

The seam sits between **classification** and **action execution** because category semantics change more frequently than the mechanics of validation, safety, auditing, and degraded handling.

Example policy:

| Category | Action | Destination | Draft allowed | Min confidence |
|---|---|---|---:|---:|
| general_q&a | draft_resolution | - | yes | 0.85 |
| product_feedback | route | product | no | 0.70 |
| compliance | route | compliance_legal | no | n/a for caution |

A future `suspected_fraud` category that routes to Fraud Operations and never drafts would add a category plus policy/classifier configuration without rewriting the core pipeline.

## 7. Safety and data boundaries

### Compliance

Compliance safety is independent from category classification. The system performs an early safety assessment that can override a classifier result. If the safety detector identifies a compliance-sensitive signal, the effective category is forced to compliance and draft execution is not reachable.

A second egress guard validates invariants after action execution:

- a compliance-sensitive decision must not contain `draft_response`;
- a category with `DraftAllowed=false` must not contain a draft;
- any member-facing draft must require human approval.

This is intentionally redundant: a classifier mistake alone is insufficient to create an unsafe member answer.

### Sensitive data

Raw member text stays in the existing authorized case system. Logs, traces, audit records, and cross-team routing payloads use IDs, reason codes, policy versions, confidence, and sanitized summaries. Routing metadata is allowlisted rather than copied wholesale. Production model adapters would minimize/redact fields that are not needed for the model task and use approved no-training/no-retention provider settings; raw request bodies would never be captured in application or tracing telemetry.

### Detection and recovery

Production monitoring would alert on any egress safety invariant violation, anomalous compliance-routing volume, routing failures, or missing audit writes. The final member-release gate is also policy-aware: human approval is necessary but cannot turn a Compliance decision into a member-facing response.

If evidence showed that an unsafe response nevertheless reached a member, the incident path is: activate the kill switch; disable member release/affected category automation; return all new work to the existing manual queue; identify affected decisions by decision ID, policy/model version, and timestamps; preserve the authoritative cases for investigation; and involve Support plus Compliance/Legal for impact assessment and remediation. Only after the invariant, test/evaluation coverage, and rollout criteria are corrected would automation be re-enabled. Because the original case already exists before triage, disabling AI does not lose member work.

## 8. Existing-system integration

### Build new

- triage service/worker;
- policy registry;
- safety and egress guards;
- model/KB adapters;
- sanitized decision/audit store;
- operational metrics and kill switch.

### Wrap

- ticket/case system: source of truth and manual fallback;
- help-center knowledge base: retrieval dependency for Q&A drafting;
- member profile service: narrow interface, queried only when a category needs it;
- ticketing/routing API: destination updates/queue assignment.

### Leave alone

- authentication/identity system;
- raw case persistence;
- current manual support workflow;
- existing member-send mechanism;
- Compliance/Legal and Product team operating queues.

## 9. Degraded mode and latency

The system fails closed toward humans.

| Failure | Safe behavior |
|---|---|
| classifier unavailable | human escalation |
| classifier below category threshold | human escalation |
| KB unavailable | human escalation |
| drafter unavailable | human escalation |
| member profile unavailable when required | human escalation; do not infer missing account context |
| routing API unavailable | retain existing case/manual queue; retry safely in production |
| audit store unavailable | do not progress automated action; retain manual path |
| compliance uncertainty | compliance/human path, never draft |

Production adapters receive per-dependency deadlines, bounded retries only for safe/idempotent operations, and circuit breakers where appropriate. Stage-level structured telemetry records `decision_id`, `intake_id`, stage name, dependency, duration, outcome, and reason code—never raw member text or unrestricted metadata. A slow response is debugged from per-stage timings and dependency health (for example, retrieval vs. model vs. routing), with p50/p95/p99 latency and timeout/degraded-mode rates monitored by dependency. The durable queue isolates bursty intake acceptance from downstream latency.

## 10. Human approval and explainability

Every member-facing Q&A draft is returned with `human_approval_required=true`, but the prototype does not treat that boolean as the security boundary. `ReleaseForMember` is an explicit final gate between an AI draft and the existing member-send workflow. It releases only a General Q&A `draft_resolution` with a matching human review in `approved` or `edited_and_approved` state. Pending/rejected reviews are blocked; a Compliance decision is blocked even if a caller supplies an approval. The release gate also requires the approver and review time to be durably written before returning a releasable payload. If audit persistence fails, release fails closed.

The decision/audit record stores decision ID, intake ID, effective category, action, confidence, reason codes, safety flags, policy version, approval state, approver identity, and timestamps. It deliberately excludes raw member text. In production this sanitized metadata would land in a dedicated, access-controlled and encrypted relational audit table (or the organization's equivalent durable audit store), separate from the raw case system. For this exercise, retention is assumed to be 365 days and should be replaced by the real Legal/Compliance retention policy.

A care lead can therefore reconstruct from the decision record why the intake drafted, routed, or escalated, which policy version was applied, whether a safety override occurred, and—when a response was released—who approved it and when. The original case remains available to authorized staff through the existing case system when deeper investigation is permitted and required.

## 11. Rollout plan

1. **Historical evaluation:** Support lead + experienced agents label representative prior cases and define unacceptable errors.
2. **Shadow mode:** AI runs independently; humans continue the current workflow. Compare category, resolution quality, and routing after the fact.
3. **Assisted Q&A:** expose AI drafts with decision reason codes and relevant KB evidence to agents; the release gate requires approve/edit/reject review before the existing send workflow can receive a response.
4. **Staged routing:** enable proven Product/Compliance routing for a limited percentage/category slice while keeping the manual queue as fallback.
5. **Scale by evidence:** increase coverage only when safety, quality, edit/reject rate, latency, and review capacity remain within agreed thresholds.
6. **Kill switch:** any safety regression immediately returns all cases to the current manual workflow.

The 80% target is treated as an outcome to earn through measured reliability, not as a launch-day automation quota.

## 12. Metrics

- category precision/recall, especially compliance false negatives;
- AI draft acceptance, edit, and rejection rates;
- time-to-approval and total resolution time;
- human-review load;
- routing accuracy;
- confidence calibration;
- degraded-mode rate by dependency;
- p50/p95/p99 triage latency;
- safety-invariant violations (target: zero);
- percentage of cases resolved with AI assistance;
- median approval handling time and one-click acceptance rate, to ensure the review queue remains operationally sustainable.

## 13. Bringing the business team along

I would start with the Customer Support lead and both experienced agents, including the skeptic, by reviewing representative historical cases and asking each person to independently define the correct category, routing decision, and what a good member resolution looks like. Disagreements expose undocumented operating policy that should be resolved before encoding it into AI behavior.

During shadow mode, agents work exactly as they do today while the AI makes independent decisions. We review disagreements together, with the skeptical veteran explicitly responsible for identifying unsafe or low-quality behavior and helping define escalation rules and acceptance criteria. The goal is not to persuade them that AI is good; it is to turn their operational expertise into tests, policies, and thresholds the system must earn its way through.

As evidence improves, the team moves from shadow mode to visible drafts and staged routing. I would publish simple scorecards for acceptance/edit rates, safety errors, routing accuracy, review load, and resolution time. Before leaving the engagement, ownership transfers through runbooks, dashboards, policy/config documentation, incident and kill-switch procedures, and designated Support owners who have already participated in evaluation and rollout.

## 14. Tradeoffs

- **Rules in prototype vs. real LLM:** rules make safety seams deterministic and inspectable in the timebox; model adapters can replace them without changing orchestration.
- **Synchronous prototype vs. async production:** synchronous HTTP is easy to review; durable async work is more appropriate for bursty production traffic and dependency latency.
- **Sanitized summaries vs. full cross-team payloads:** data minimization reduces exposure; authorized teams can follow the case ID to the source system when full context is permitted.
- **Conservative failure behavior:** manual fallback increases human load during outages but protects members and compliance obligations.

## 15. With more time

- contract/integration tests for real ticketing, profile, KB, and model adapters;
- richer PII detection and field-level data classification;
- offline evaluation dataset and confidence calibration;
- queue/idempotency implementation;
- approval UI and integration with the existing member-send system (the release-domain gate is implemented in the prototype);
- metrics dashboards and alert definitions;
- chaos/failure testing;
- policy change management and version rollout;
- load testing against expected burst profiles.
