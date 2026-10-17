# Why Protocol Buffers and the Open Knowledge Format Belong Together

*A Diátaxis Explanation of how strongly-typed RPC schemas and human/agent-readable knowledge graphs complete each other.*

---

## The Two Silos of Modern Engineering Knowledge

Every engineering organization has two kinds of truth, and today they live in separate worlds that cannot talk to each other:

1. **The Wire & Schema World (Protocol Buffers, Buf, gRPC, BigQuery):**
   Machine-enforced, compiler-verified structure. Your `.proto` files know that `OrderService.GetOrder` takes a `GetOrderRequest` and returns an `Order`, that `Order.status` is an `OrderStatus` enum, and that `LegacyOrderV0` is marked `option deprecated = true`.
2. **The Institutional Knowledge World (Markdown, ADRs, Runbooks, Policies, Metrics):**
   Human- and agent-readable context. Why does `Order.status` transition from `PENDING` to `SHIPPED`? Which finance policy governs when an `Order` counts toward recognized revenue? Who last verified that the fulfillment SLA runbook still matches the production service?

When an AI agent (or a new engineer) tries to answer a cross-cutting question—*"If we deprecate this field on `Order`, which services, metrics, and compliance policies are affected, and is the documentation we're reading even current?"*—it hits a wall:

- **Protobuf compilers (`protoc`, `buf`)** see the exact type graph of services, messages, and enums, **but have no vocabulary for trust, verification tiers, staleness windows, or links to prose policies and SQL metrics.**
- **Markdown wikis and RAG pipelines** hold the prose policies and runbooks, **but treat API schemas as opaque code blocks or external links, losing the structural graph of RPCs and messages.**

The **[Open Knowledge Format (OKF) v0.2](https://github.com/GoogleCloudPlatform/open-knowledge-format)** was created to solve the institutional-knowledge side of this divide: a vendor-neutral specification where a directory of Markdown files with YAML frontmatter *is* a queryable, trust-bearing knowledge graph.

**`okf-proto` bridges the two worlds.**

---

## How Protobuf and OKF Intersect on Two Axes

When you ask *"What is the relationship between Protocol Buffers and OKF?"*, the answer is not one mapping—it is **two complementary directions**:

```text
 ┌─────────────────────────────────────────────────────────────────────────┐
 │ Axis 1: Schema Ingestion (.proto / Buf  ──►  OKF v0.2 Bundle)           │
 │                                                                         │
 │   orders.proto ──(buf build / protoc-gen-okf)──► services/OrderService  │
 │                                                  messages/Order         │
 │                                                  enums/OrderStatus      │
 └─────────────────────────────────────────────────────────────────────────┘

 ┌─────────────────────────────────────────────────────────────────────────┐
 │ Axis 2: Wire & Transport (OKF v0.2 Bundle  ◄──►  okf.v1.Bundle Proto)   │
 │                                                                         │
 │   OKF Markdown Tree ◄──(okf-proto export/import)──► bundle.binpb/.json  │
 └─────────────────────────────────────────────────────────────────────────┘
```

### Axis 1: Protobuf Schemas *as a Source Corpus* (`from-descriptor` & `protoc-gen-okf`)

In OKF's origin story, a data catalog ingests a BigQuery dataset (`datasets/`, `tables/`) and turns every table into a Markdown concept document that humans and agents can enrich, verify, and link to business metrics.

A Protobuf schema (or a [Buf Schema Registry](https://buf.build) module) is an even richer source of structural truth than a database table:
- **Every `service`, `message`, and `enum` is a natural OKF Concept.**
- **Every typed field and RPC signature is an explicit directed graph edge.** When `OrderService.GetOrder` returns `Order`, and `Order` has a field `repeated LineItem items` and `OrderStatus status`, those are not guesses by an LLM—they are compiler-verified edges:
  - `services/acme/orders/v1/OrderService` → `messages/acme/orders/v1/GetOrderRequest`
  - `services/acme/orders/v1/OrderService` → `messages/acme/orders/v1/Order`
  - `messages/acme/orders/v1/Order` → `messages/acme/orders/v1/LineItem`
  - `messages/acme/orders/v1/Order` → `enums/acme/orders/v1/OrderStatus`
- **Comments and lifecycle flags already exist in the AST.** When compiled with `buf build` or `protoc --include_source_info`, the `FileDescriptorSet` carries leading and trailing source comments (`SourceCodeInfo`) alongside `option deprecated = true` markers.

By projecting a `FileDescriptorSet` into an OKF v0.2 bundle, several things happen immediately that `.proto` files cannot do on their own:

1. **Unified Graph Traversal Across Code and Prose:**
   Once your Protobuf services and messages are OKF concepts, an architecture decision record (`decisions/adr-004-order-cancellation.md`) or a revenue metric (`metrics/net-revenue.md`) can link directly to `[Order](/messages/acme/orders/v1/Order.md)`. Graph tools (`okf graph`, `binder graph`, `binder mcp`'s `query_graph`, or Spanner Graph via `binder project`) can now traverse from a finance policy → to a BigQuery table → to the Protobuf `Order` message → to the `OrderService` RPC in a single bounded graph query.
2. **Entrypoints vs. Dead Schema Detection for Free:**
   When you run `binder review` over a bundle generated by `okf-proto`, the graph topology immediately classifies your API surface:
   - **Entrypoints (0 inbound links, >0 outbound links):** Your `Protobuf Service` concepts (`OrderService`), because RPC services are the roots that reference request/response messages.
   - **Orphans (0 inbound links, 0 outbound links):** Unreferenced, standalone messages (such as a deprecated `LegacyOrderV0` that no RPC or parent message uses anymore).
3. **Re-Rooting-Stable Graph Identity (`resource: proto://<fqn>`):**
   Every concept emitted by `okf-proto` carries a canonical frontmatter identity (`resource: proto://acme.orders.v1.Order`). When projecting the bundle to a relational property graph like Google Cloud Spanner Graph (`binder project --id-key resource`), every node and foreign-key edge is keyed by its language-neutral Protobuf fully-qualified name (`proto://acme.orders.v1.Order`) rather than a filesystem path—achieving 100% re-rooting stability (`re_rooting_stable: true`, `path_fallback_count: 0`).
4. **Trust & Governance Layer Over Generated Schemas:**
   Per OKF §5, `okf-proto` stamps `generated: { by: "okf-proto/0.1.0", at: "..." }` and `sources: [{ id: "1", resource: "acme/orders/v1/orders.proto" }]`, and **never** fabricates a `verified` stamp. Every generated schema concept starts in the `unverified` tier. Teams can then layer human attestations (`verified: [{ by: "human:api-owner@acme", at: "..." }]`) or staleness policies (`stale_after`) onto critical API contracts without touching the `.proto` wire definition.

---

### Axis 2: Protobuf *as the Wire Envelope* for OKF Bundles (`okf.v1.Bundle`)

While Axis 1 brings Protobuf schemas *into* OKF, Axis 2 brings OKF bundles *into* Protobuf-native systems.

On disk and in Git, OKF's "files are the format" rule (a directory of `.md` files with YAML frontmatter) is unbeatable for human review, `git diff`, and direct agent file reading.

However, distributed systems—gRPC microservices, Vertex AI / Dataplex indexing pipelines, remote MCP servers, and content-addressable caches—often need to **ship, validate, or index an entire OKF bundle as a single, strongly-typed payload** without untarring a directory tree and re-parsing YAML frontmatter and Markdown links at every hop.

Historically, trying to represent an open-world YAML+Markdown format in Protobuf fails for one of two reasons:
- **The Rigid Schema Trap:** If the Protobuf message only models known fields (`type`, `title`, `status`), it drops arbitrary producer extension keys—violating OKF §4.1 (*"consumers SHOULD preserve unknown keys"*).
- **The Re-Encoding Trap:** If the Protobuf message parses YAML into a map and re-encodes it on import, it scrambles top-level key order, reflows folded scalars, drops YAML comments, and retypes timestamps.

`okf.v1.Bundle` (`proto/okf/v1/bundle.proto`) solves both traps by design:

1. **Typed First-Class Projections for Fast Indexing:**
   `okf.v1.Concept` exposes strongly-typed Protobuf fields for every spec-defined construct: `id`, `rel_path`, `type`, `title`, `description`, `resource`, `tags`, `sources`, `usage_window`, `generated`, `verified`, `status`, `stale_after`, pre-extracted `links` (with `resolved` status), and pre-computed `derived` trust (`tier`, `stale`, `attested`). A gRPC service or vector indexer can filter on `concept.derived.tier == "human-reviewed"` or traverse `concept.links` directly from the deserialized Protobuf message in microseconds.
2. **Open-World Extension Survival (`frontmatter` + `frontmatter_key_order`):**
   Alongside the typed fields, `Concept` carries the full top-level YAML mapping in `google.protobuf.Struct frontmatter` plus `repeated string frontmatter_key_order`. Any custom key a producer wrote survives in both binary `.binpb` and canonical ProtoJSON, in the exact key order the author chose.
3. **Lossless Byte-Identity (`original_frontmatter`):**
   Finally, `Concept` carries `bytes original_frontmatter`. When a bundle is exported from disk to `.binpb`/`.json` and imported back to disk without mutating its frontmatter, `okf-proto import` re-emits the original YAML bytes verbatim—preserving comments, quoting style, and spacing byte-for-byte (`diff -ru` is empty). When a program *does* construct or mutate a `Concept` in memory (leaving `original_frontmatter` empty), `okf-proto import` deterministically encodes the YAML frontmatter following `frontmatter_key_order`.

---

## Summary: When to Use Which Tool

| Goal | Command / Package | What Happens |
| --- | --- | --- |
| Turn a `.proto` repo or Buf module into an OKF bundle | `buf build -o fds.binpb && okf-proto from-descriptor fds.binpb -o ./bundle` *(or `buf generate` with `protoc-gen-okf`)* | Compiles `.proto` AST + comments into linked `Protobuf Service`, `Protobuf Message`, and `Protobuf Enum` OKF concepts + directory `index.md` files. |
| Validate, review, or query the resulting bundle | `okf validate ./bundle` or `binder review ./bundle` or `binder mcp` | Validates §11 conformance (0 errors, 0 warnings), identifies service entrypoints and unused orphan messages, and serves bounded graph queries over MCP. |
| Project the Protobuf + prose knowledge graph into Spanner Graph | `binder project ./bundle --out ./spanner --id-key resource` | Emits `schema.ddl`, `nodes.csv`, `edges.csv`, `load.sql`, `node_verified.csv`, and `derivation.sql` keyed by `proto://<fqn>`. |
| Ship or cache an OKF bundle over gRPC / Protobuf wire or ProtoJSON | `okf-proto export ./bundle -o bundle.binpb` / `okf-proto import bundle.binpb -o ./bundle` | Serializes the entire bundle (concepts, reserved files, trust signals, links, and raw YAML bytes) into a single deterministic `.binpb` or `.json` artifact with byte-identical round-trip. |
