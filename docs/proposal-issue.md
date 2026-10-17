## Summary

Following the pattern established by [`connectors/gcp-knowledge-catalog.md`](https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/main/connectors/gcp-knowledge-catalog.md) (PR #3), we have built an open-source, Apache-2.0 Protocol Buffers / [Buf](https://buf.build) connector for OKF v0.2 ([`ghchinoy/okf-proto`](https://github.com/ghchinoy/okf-proto)) and would like to propose adding a connector guide (`connectors/protobuf-buf.md`) to this repository.

**No changes to `SPEC.md` are proposed.** Everything uses the existing OKF v0.2 vocabulary (`type`, `resource`, `sources` with `[^1]` footnote joins, `generated`, `status`, `stale_after`, and §6.1 bundle-relative markdown links).

---

## Why Protocol Buffers / Buf + OKF?

Today, OKF's reference bundles demonstrate ingesting analytical data schemas (BigQuery datasets and tables) into human- and agent-readable concept graphs.

In service-oriented architectures, the equivalent structural truth lives in **Protocol Buffers (`.proto` files) and Buf modules** — and bridging them with OKF solves two practical problems:

### 1. `.proto` / Buf Schemas as an Offline, Credential-Free OKF Producer (`protoc-gen-okf` & `from-descriptor`)
A compiled `google.protobuf.FileDescriptorSet` (produced by `buf build -o descriptors.binpb` or `protoc --include_source_info --descriptor_set_out=...`) already contains the complete AST, source comments (`SourceCodeInfo`), deprecation flags, and type cross-references of an API surface:

- **Concepts:** Every `service`, `message`, and `enum` becomes an OKF v0.2 concept (`type: Protobuf Service`, `Protobuf Message`, `Protobuf Enum`) with `resource: proto://<package>.<Name>`.
- **Graph edges (§6.1):** Every RPC request/response type and message/enum field reference is emitted as a bundle-relative markdown link (`[Order](/messages/acme/orders/v1/Order.md)`). Running the reference `visualize` command (`viz.html`), `okf graph`, or `binder graph` immediately renders the service/message dependency topology — and prose architecture docs, runbooks, or metrics in the same bundle can link directly to those message and service concepts.
- **Lifecycle & Provenance (§5):**
  - `option deprecated = true` on a file, service, message, or enum maps directly to `status: deprecated` (§5.4).
  - Source `.proto` paths populate `sources: [{ id: "1", resource: "<path>.proto", title: "<path>.proto" }]` joined to `[^1]` in the body (§5.1).
  - `generated` is stamped with the producer actor and ISO 8601 UTC timestamp (`SOURCE_DATE_EPOCH`-aware, §5.2).
  - `verified` is never fabricated: generated concepts start in the `unverified` tier (§5.3) until reviewed by a process or human.

### 2. Bidirectional OKF Bundle ↔ Protobuf Wire / ProtoJSON Transport (`okf.v1.Bundle`)
For gRPC services, indexing pipelines, and caches that need to exchange an entire OKF v0.2 bundle as a single strongly-typed payload without losing OKF's open-world guarantees (§4.1 / §11), `okf-proto` defines `proto/okf/v1/bundle.proto`:

- Exposes typed fields for `id`, `rel_path`, `type`, `title`, `description`, `resource`, `tags`, `sources`, `usage_window`, `generated`, `verified`, `status`, `stale_after`, extracted `links`, and derived trust (`tier`, `stale`, `attested`).
- Preserves arbitrary producer extension keys via `google.protobuf.Struct frontmatter` + `repeated string frontmatter_key_order` (§4.1 *"consumers SHOULD preserve unknown keys"*).
- Carries optional `bytes original_frontmatter` so an `export` → `import` round-trip over an unmutated bundle is **byte-identical** (`diff -ru` exits `0` with no YAML reformatting or timestamp coercion, aligning with PR #6).

---

## Verification

Every bundle emitted by `okf-proto` (`from-descriptor`, `protoc-gen-okf`, and `export`/`import` round-trips) is validated in CI against:
- this specification's §11 rules via [`okfcli/okf`](https://github.com/okfcli/okf) v0.5.0 (`valid: true, errors: 0, warnings: 0`), and
- [`ghchinoy/binder`](https://github.com/ghchinoy/binder) v0.6.0 (`binder validate --strict`, 0 findings).

Minimal reproduction:

```bash
go install github.com/ghchinoy/okf-proto/cmd/okf-proto@v0.1.0
go install github.com/ghchinoy/okf-proto/cmd/protoc-gen-okf@v0.1.0

# 1. Generate an OKF v0.2 bundle from a .proto descriptor set (or via `buf generate`)
protoc --proto_path=testdata --include_imports --include_source_info \
  --descriptor_set_out=/tmp/orders.fds.binpb acme/orders/v1/orders.proto
okf-proto from-descriptor /tmp/orders.fds.binpb -o /tmp/orders-bundle

# 2. Export to binary Protobuf (.binpb) or ProtoJSON (.json) and verify lossless round-trip
okf-proto export /tmp/orders-bundle -o /tmp/orders.binpb
okf-proto import /tmp/orders.binpb -o /tmp/orders-roundtrip
diff -ru /tmp/orders-bundle /tmp/orders-roundtrip   # empty
```

## Proposed Contribution

If this looks useful to the project, I'd be happy to open a small PR adding [`connectors/protobuf-buf.md`](https://github.com/ghchinoy/okf-proto/blob/main/docs/connector-protobuf-buf.md) (modeled directly on `connectors/gcp-knowledge-catalog.md`, including round-trip verification and explicit limitations).

- **Connector guide draft:** [`docs/connector-protobuf-buf.md`](https://github.com/ghchinoy/okf-proto/blob/main/docs/connector-protobuf-buf.md)
- **Design rationale & architecture explainer:** [`docs/why-okf-proto.md`](https://github.com/ghchinoy/okf-proto/blob/main/docs/why-okf-proto.md)
- **Repository & proto schema:** [`ghchinoy/okf-proto`](https://github.com/ghchinoy/okf-proto) (`proto/okf/v1/bundle.proto`)
