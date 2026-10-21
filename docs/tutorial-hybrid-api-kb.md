# Tutorial: Building a Hybrid API & Institutional Knowledge Graph with `okf-proto` and `binder`

A hands-on, runnable walkthrough. You will start with a `.proto` RPC schema, project it into an [Open Knowledge Format (OKF) v0.2](https://github.com/GoogleCloudPlatform/open-knowledge-format) bundle using `okf-proto`, layer human-reviewed Architecture Decision Records (ADRs) and Finance Metrics that link directly to the Protobuf messages, stamp CI attestations, and run cross-domain blast-radius queries across the unified graph.

---

## Prerequisites

Install `okf-proto`, `binder`, and `okf`:

```bash
go install github.com/ghchinoy/okf-proto/cmd/okf-proto@latest
go install github.com/ghchinoy/okf-proto/cmd/protoc-gen-okf@latest
go install github.com/ghchinoy/binder@latest
go install github.com/okfcli/okf/cmd/okf@latest
```

Pin `SOURCE_DATE_EPOCH` so every timestamp in this walkthrough is deterministic:

```bash
export SOURCE_DATE_EPOCH=1700000000
WORK="$(mktemp -d)"
```

---

## Step 1: Ingest a `.proto` Schema into an OKF v0.2 Bundle

From the `okf-proto` repository root, compile `testdata/acme/orders/v1/orders.proto` (which defines `OrderService`, `Order`, `LineItem`, `GetOrderRequest`, `OrderStatus`, and a deprecated `LegacyOrderV0`) with `protoc` (or `buf build`) and generate the initial OKF bundle:

```bash
protoc \
  --proto_path=testdata \
  --include_imports \
  --include_source_info \
  --descriptor_set_out="$WORK/orders.fds.binpb" \
  acme/orders/v1/orders.proto

okf-proto from-descriptor "$WORK/orders.fds.binpb" -o "$WORK/kb"
```

Inspect the generated bundle with `binder review`:

```bash
binder review "$WORK/kb" --today 2026-08-15
```

```text
binder review
  bundle: /tmp/.../kb
  concepts: 6
  by type:
    Protobuf Enum: 1
    Protobuf Message: 4
    Protobuf Service: 1
  trust tiers:
    human-reviewed: 0
    machine-confirmed: 0
    unverified: 6
  stale (as of 2026-08-15): 0
  attested computations: 0
  unparsed frontmatter (recovered as body): 0
  entrypoints (no inbound links): 1
    services/acme/orders/v1/OrderService
  orphans (no inbound or outbound links): 1
    messages/acme/orders/v1/LegacyOrderV0
  unresolved links: 0
```

Notice what the graph topology reveals immediately:
- **`services/acme/orders/v1/OrderService` is an entrypoint:** It has no inbound links and links outbound to `GetOrderRequest` and `Order`.
- **`messages/acme/orders/v1/LegacyOrderV0` is a true orphan:** It is marked `status: deprecated` in frontmatter (from `option deprecated = true` in `.proto`) and has zero inbound or outbound edges—proving no RPC or parent message references it.

---

## Step 2: Layer Prose Architecture Decisions and Business Metrics

In a real system, `.proto` schemas are governed by architectural decisions and feed downstream analytical metrics. Add two Markdown documents—an ADR and a Finance Metric—that link directly to the Protobuf concepts:

```bash
mkdir -p "$WORK/kb/decisions" "$WORK/kb/metrics"

cat > "$WORK/kb/decisions/adr-012-order-cancellation.md" << 'EOF'
---
type: Decision
title: "ADR-012: Order Cancellation Semantics"
description: Defines when a customer order may transition to ORDER_STATUS_CANCELLED.
status: stable
generated: { by: human:arch-board, at: 2026-07-01T10:00:00Z }
verified:
  - { by: human:staff-eng@acme, at: 2026-07-02T15:00:00Z }
---

# ADR-012: Order Cancellation Semantics

Governs cancellation transitions on [OrderService](/services/acme/orders/v1/OrderService.md)
and the [OrderStatus](/enums/acme/orders/v1/OrderStatus.md) lifecycle enum.
EOF

cat > "$WORK/kb/metrics/recognized-revenue.md" << 'EOF'
---
type: Metric
title: Recognized Revenue
description: Net revenue from fulfilled customer orders.
status: stable
stale_after: 2026-12-31T00:00:00Z
generated: { by: human:finance-fpa, at: 2026-07-10T09:00:00Z }
verified:
  - { by: human:controller@acme, at: 2026-07-11T11:00:00Z }
---

# Recognized Revenue

Sums [LineItem](/messages/acme/orders/v1/LineItem.md) unit prices on every
[Order](/messages/acme/orders/v1/Order.md) whose [OrderStatus](/enums/acme/orders/v1/OrderStatus.md)
has reached `ORDER_STATUS_SHIPPED`.
EOF
```

Regenerate the bundle's `index.md` navigation with type grouping and backlinks:

```bash
binder index "$WORK/kb" --group-by-type --include-backlinks --include-graph
```

---

## Step 3: Attest the Protobuf Schema Concepts with CI Provenance

The generated Protobuf concepts started in the `unverified` tier (`okf-proto` never fabricates a `verified` stamp). After `buf breaking` and `buf lint` pass in CI, stamp a machine attestation onto the `services/` and `messages/` trees using `binder enrich`:

```bash
binder enrich "$WORK/kb/services" --verified-by "process:buf-breaking-ci"
binder enrich "$WORK/kb/messages" --verified-by "process:buf-breaking-ci"
```

Now run `binder review` across the hybrid bundle:

```bash
binder review "$WORK/kb" --today 2026-08-15
```

```text
binder review
  bundle: /tmp/.../kb
  concepts: 8
  by type:
    Decision: 1
    Metric: 1
    Protobuf Enum: 1
    Protobuf Message: 4
    Protobuf Service: 1
  trust tiers:
    human-reviewed: 2
    machine-confirmed: 5
    unverified: 1
  stale (as of 2026-08-15): 0
  attested computations: 0
  unparsed frontmatter (recovered as body): 0
  entrypoints (no inbound links): 2
    decisions/adr-012-order-cancellation
    metrics/recognized-revenue
  orphans (no inbound or outbound links): 1
    messages/acme/orders/v1/LegacyOrderV0
  unresolved links: 0
```

All three OKF §5.3 trust tiers are now visible in one bundle:
- **`human-reviewed` (2):** The ADR (`human:staff-eng@acme`) and the Recognized Revenue metric (`human:controller@acme`).
- **`machine-confirmed` (5):** The `services/` and `messages/` concepts attested by `process:buf-breaking-ci`.
- **`unverified` (1):** `enums/acme/orders/v1/OrderStatus`, which has not yet been attested.

### Gate on a Minimum Trust Tier Floor in CI (`--require-tier`)

While OKF v0.2 consumers never reject a bundle for lacking verification, a CI pipeline publishing a bundle can enforce an explicit minimum trust tier floor (`unverified`, `machine-confirmed`, or `human-reviewed`) with `binder review --require-tier` (v0.8.0+). When any concept falls below the required tier, `binder review` exits `1` and lists *why* each concept fell short:

```bash
binder review "$WORK/kb" --today 2026-08-15 --require-tier machine-confirmed
echo "exit=$?"
```

```text
binder review
  bundle: /tmp/.../kb
  concepts: 8
  by type:
    Decision: 1
    Metric: 1
    Protobuf Enum: 1
    Protobuf Message: 4
    Protobuf Service: 1
  trust tiers:
    human-reviewed: 2
    machine-confirmed: 5
    unverified: 1
  below required tier (machine-confirmed): 1
    enums/acme/orders/v1/OrderStatus (unverified: no verified entry)
  stale (as of 2026-08-15): 0
  attested computations: 0
  unparsed frontmatter (recovered as body): 0
  entrypoints (no inbound links): 2
    decisions/adr-012-order-cancellation
    metrics/recognized-revenue
  orphans (no inbound or outbound links): 1
    messages/acme/orders/v1/LegacyOrderV0
  unresolved links: 0
binder: review found 1 concept(s) below required tier "machine-confirmed" (--require-tier): enums/acme/orders/v1/OrderStatus (unverified: no verified entry)
exit=1
```

If a production release pipeline requires `human-reviewed` across the entire bundle, the shortfall list distinguishes the unverified enum (`no verified entry`) from the 5 CI-attested Protobuf concepts (`verified only by non-human actor(s): process:buf-breaking-ci`), turning a red CI build into an immediate review checklist.

---

## Step 4: Cross-Domain Blast-Radius & Lineage Queries

Because the ADR, the Finance Metric, and the Protobuf schema share one OKF link graph, an agent (via `binder mcp`'s `query_graph` tool or the Go SDK's `Service.GraphQuery`) can answer questions that span prose and `.proto` definitions:

1. **Blast Radius (`op: neighbors`, `direction: in` on `enums/acme/orders/v1/OrderStatus`):**
   *"If we modify `OrderStatus`, what breaks across our APIs, architecture decisions, and finance metrics?"*
   - Returns **3 dependents across 3 domains**:
     - `messages/acme/orders/v1/Order` (`Protobuf Message`)
     - `decisions/adr-012-order-cancellation` (`Decision`, `human-reviewed`)
     - `metrics/recognized-revenue` (`Metric`, `human-reviewed`)
2. **Lineage Path (`op: path`, `direction: both` from `metrics/recognized-revenue` to `services/acme/orders/v1/OrderService`):**
   *"How does `OrderService` affect `Recognized Revenue`?"*
   - Returns the 2-hop path:
     `metrics/recognized-revenue` → `messages/acme/orders/v1/Order` ← `services/acme/orders/v1/OrderService`

You can run this entire workflow in-process in Go from the `binder` repository:

```bash
go run ./examples/proto
```

---

## Step 5: Export to Spanner Graph or Protobuf Wire Format

Finally, project the hybrid bundle into Google Cloud Spanner Graph DDL/CSVs or a single `.binpb` Protobuf wire payload:

```bash
# Offline Spanner SQL/PGQ property-graph projection:
binder project "$WORK/kb" --out "$WORK/spanner" --today 2026-08-15

# Lossless binary Protobuf wire export & round-trip check:
okf-proto export "$WORK/kb" -o "$WORK/kb.binpb" --today 2026-08-15
okf-proto import "$WORK/kb.binpb" -o "$WORK/kb-roundtrip"
diff -ru "$WORK/kb" "$WORK/kb-roundtrip"   # empty (byte-identical)
```
