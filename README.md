# Support Triage & Resolution Agent

Focused Go prototype for the Forward Deployed Engineer take-home case study.

The submission intentionally prioritizes the architecture around AI classification and drafting—not model sophistication. The existing support case system remains the source of truth and manual safety net; this service adds triage, category policy, structural safety gates, sanitized routing, auditability, and a modeled human approval/release boundary.

## Run

```bash
go test ./...
go vet ./...
go run ./cmd/server
```

Example triage request:

```bash
curl -X POST http://localhost:8080/v1/triage \
  -H 'Content-Type: application/json' \
  -d '{"id":"case-1","member_id":"member-1","text":"How do I update my profile?"}'
```

## What the prototype demonstrates

- one shared triage pipeline for General Q&A, Product Feedback, and Compliance;
- configuration-driven category policy and category-specific confidence thresholds;
- an independent safety assessment that can override an incorrect classifier;
- a final egress invariant check before an action can progress;
- fail-closed degraded behavior for low confidence and dependency failures;
- sanitized/allowlisted cross-team routing context;
- pre-side-effect audit persistence for automated routing;
- an explicit human approval/release gate for member-facing drafts;
- audit updates that record who approved a response and when;
- CI tests that intentionally pressure-test compliance and sensitive-data invariants.

`POST /v1/triage` is synchronous only to make the exercise easy to run and review. The RFC proposes queue-backed asynchronous triage for production bursts.

The prototype does **not** implement a new member-send mechanism. `Service.ReleaseForMember` models the final approval boundary and returns a releasable payload only after a valid human approval has been durably audited; the existing member-send workflow remains deliberately outside this service.

## Repository layout

```text
.
├── .github/workflows/test.yml
├── AI_LEVERAGE_LOG.md
├── README.md
├── RFC.md
├── cmd/server/main.go
└── internal/triage/
    ├── fakes.go
    ├── interfaces.go
    ├── policies.go
    ├── redact.go
    ├── release.go
    ├── release_test.go
    ├── safety.go
    ├── service.go
    ├── service_test.go
    └── types.go
```

See `RFC.md` for assumptions, architecture, rollout, business adoption, tradeoffs, and deferred production work.
# bankrate-case-study
