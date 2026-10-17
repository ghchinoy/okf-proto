# Generating and Exchanging OKF Bundles with Protocol Buffers and Buf

Generate a conformant OKF v0.2 bundle from `.proto` schemas (via [`buf`][buf] or
[`protoc`][protoc]) and exchange OKF bundles as deterministic Protocol Buffer
payloads (`.binpb` / ProtoJSON), using **`okf-proto`** and
**`protoc-gen-okf`** from [`ghchinoy/okf-proto`][okf-proto].

Read [Limitations](#limitations) first.

[buf]: https://buf.build
[protoc]: https://protobuf.dev
[okf-proto]: https://github.com/ghchinoy/okf-proto

## Prerequisites

Go 1.26+ and either [`buf`][buf] or [`protoc`][protoc].

Install the CLI and `protoc` plugin:

```bash
go install github.com/ghchinoy/okf-proto/cmd/okf-proto@latest
go install github.com/ghchinoy/okf-proto/cmd/protoc-gen-okf@latest
```

Both are static binaries with no runtime dependencies and no network calls.

## 1. Generate an OKF bundle from `.proto` schemas

Compile a `.proto` tree or Buf module with source comments (`SourceCodeInfo`)
into a `FileDescriptorSet`, then convert it to an OKF v0.2 bundle:

```bash
# Using buf:
buf build -o descriptors.binpb
okf-proto from-descriptor descriptors.binpb -o ./catalog

# Or using protoc directly:
protoc --proto_path=proto --include_imports --include_source_info \
  --descriptor_set_out=descriptors.binpb proto/acme/orders/v1/orders.proto
okf-proto from-descriptor descriptors.binpb -o ./catalog
```

Alternatively, wire `protoc-gen-okf` directly into `buf.gen.yaml` so `buf generate`
writes the OKF bundle in one step:

```yaml
version: v2
plugins:
  - local: protoc-gen-okf
    out: catalog
```

### How `.proto` constructs map to OKF v0.2

| Protobuf construct | OKF v0.2 representation |
| --- | --- |
| `service <Name>` | `services/<pkg>/<Name>.md` (`type: Protobuf Service`) with an RPC method table linking request/response types |
| `message <Name>` | `messages/<pkg>/<Name>.md` (`type: Protobuf Message`) with a field table (`#`, name, cardinality, type, description) |
| `enum <Name>` | `enums/<pkg>/<Name>.md` (`type: Protobuf Enum`) with an enum value table |
| Message / enum field references & RPC inputs/outputs | Bundle-relative markdown links (`[Order](/messages/acme/orders/v1/Order.md)`), forming navigable graph edges (§6.1) |
| Leading / trailing comments (`SourceCodeInfo`) | Concept `description` (first sentence) and body prose |
| `option deprecated = true` | `status: deprecated` in frontmatter (§5.4); otherwise `status: stable` |
| Source `.proto` file path | `sources: [{ id: "1", resource: "<path>.proto", title: "<path>.proto" }]` joined to `[^1]` in the body (§5.1) |
| Generation provenance | `generated: { by: "okf-proto/<version>", at: "<ISO-8601-UTC>" }` (honours `SOURCE_DATE_EPOCH`, §5.2) |
| Directory hierarchy | Root `index.md` (`okf_version: "0.2"`, §12) plus per-directory `index.md` navigation (§8) |

## 2. Export an OKF bundle to Protobuf wire format or ProtoJSON

Any conformant OKF v0.2 bundle — whether generated from `.proto` files, from
BigQuery by the reference agent, or authored by hand — can be serialized to the
`okf.v1.Bundle` Protobuf schema (`proto/okf/v1/bundle.proto`):

```bash
# Deterministic binary wire format (.binpb)
okf-proto export ./catalog -o catalog.binpb --today 2026-10-17

# Canonical ProtoJSON (.json)
okf-proto export ./catalog -o catalog.json --today 2026-10-17
```

## 3. Verify the round-trip

Rehydrate the bundle from either `.binpb` or `.json` and diff against the
source directory:

```bash
okf-proto import catalog.binpb -o ./catalog-roundtrip
diff -ru ./catalog ./catalog-roundtrip     # should be empty
```

Because `okf.v1.Concept` carries both the structured frontmatter (`frontmatter`
`google.protobuf.Struct` + `frontmatter_key_order`) and `original_frontmatter`
bytes, an unmutated export → import round-trip is byte-identical. When
`original_frontmatter` is omitted (for example, when a concept is constructed or
modified in memory by a gRPC service), `okf-proto import` deterministically
encodes the YAML frontmatter in `frontmatter_key_order`.

## Limitations

- **Schema-to-OKF (`from-descriptor` / `protoc-gen-okf`) is one-way.** Converting
  `.proto` descriptors into an OKF bundle is an ingestion projection for
  discovery, documentation, and graph traversal; `okf-proto` does not compile
  markdown tables back into `.proto` IDL files (wire numbers, custom field
  options, and `reserved` ranges are not round-tripped from prose).
- **Comments require `SourceCodeInfo`.** When using `protoc --descriptor_set_out`,
  pass `--include_source_info` so leading/trailing `.proto` comments are
  available (`buf build` includes source info by default). Without it,
  concepts receive a synthesized fallback description.
- **Non-`.md` assets are not carried in `okf.v1.Bundle`.** Like the Knowledge
  Catalog connector, `okf-proto export` and `import` operate on `.md` concept
  and reserved files (`index.md`, `log.md`); companion static assets (`viz.html`,
  images) are ignored.
- **Never fabricates verification.** `from-descriptor` and `protoc-gen-okf`
  stamp `generated` and `sources`, and never emit `verified` — every generated
  concept starts in the `unverified` tier until attested by a process or human
  reviewer (§5.3).
