# okf-proto

Bidirectional [Open Knowledge Format (OKF) v0.2](https://github.com/GoogleCloudPlatform/open-knowledge-format) ↔ Protocol Buffers / [Buf](https://buf.build) toolkit and Go SDK.

![license](https://img.shields.io/badge/license-Apache--2.0-blue)
![go](https://img.shields.io/badge/go-1.26+-00ADD8)

`okf-proto` provides two complementary bridges between the Open Knowledge Format (OKF) v0.2 and the Protocol Buffers ecosystem, with **zero dependency on any specific OKF CLI**:

1. **Protobuf / Buf Schema → OKF v0.2 Bundle (`from-descriptor` & `protoc-gen-okf`):**
   Converts compiled `.proto` schemas (`google.protobuf.FileDescriptorSet` from `buf build` or `protoc`, or directly via `protoc --plugin=protoc-gen-okf` / `buf generate`) into a conformant OKF v0.2 bundle. Every Protobuf `Service`, `Message`, and `Enum` becomes an addressable OKF concept document; every typed field and RPC request/response becomes a resolved `[Type](/messages/...md)` graph edge; `SourceCodeInfo` doc comments become concept descriptions and prose; and `option deprecated = true` maps to OKF `status: deprecated`.
2. **OKF v0.2 Bundle ↔ Protobuf Wire / ProtoJSON (`export` & `import`):**
   Defines a canonical `okf.v1.Bundle` Protobuf wire schema (`proto/okf/v1/bundle.proto`) and a lossless round-trip codec (`pkg/bundle`) that serializes any on-disk OKF v0.2 bundle to deterministic binary `.binpb` or canonical `.json` (preserving all §5 trust/provenance signals, unknown extension frontmatter keys, key order, and original frontmatter bytes) and rehydrates it back to disk byte-for-byte.

Every bundle produced by `okf-proto` is differential-validated against both [`okfcli/okf`](https://github.com/okfcli/okf) (v0.5.0, zero errors and zero warnings) and [`ghchinoy/binder`](https://github.com/ghchinoy/binder) (`validate --strict`, zero findings).

---

## Installation

```bash
go install github.com/ghchinoy/okf-proto/cmd/okf-proto@latest
go install github.com/ghchinoy/okf-proto/cmd/protoc-gen-okf@latest
```

Or build from source:

```bash
git clone https://github.com/ghchinoy/okf-proto.git
cd okf-proto
make build    # -> bin/okf-proto, bin/protoc-gen-okf
make check    # gofmt + go vet + buf lint + go test -race ./...
make interop  # end-to-end differential validation with okf and binder
```

---

## Usage

### 1. Generate an OKF v0.2 Bundle from `.proto` / Buf Schemas

#### Option A: Via `buf build` or `protoc --descriptor_set_out`

```bash
# With Buf:
buf build -o descriptors.binpb
okf-proto from-descriptor descriptors.binpb -o ./okf-bundle

# Or with protoc:
protoc --proto_path=proto --include_imports --include_source_info \
  --descriptor_set_out=descriptors.binpb proto/acme/orders/v1/orders.proto
okf-proto from-descriptor descriptors.binpb -o ./okf-bundle
```

#### Option B: As a `buf generate` / `protoc` plugin (`protoc-gen-okf`)

Add `protoc-gen-okf` to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-okf
    out: okf-bundle
```

Then run `buf generate` (or `protoc --okf_out=./okf-bundle ...`).

#### Emitted Bundle Structure

For a package `acme.orders.v1` defining `OrderService`, `Order`, `LineItem`, `GetOrderRequest`, and `OrderStatus`:

```text
okf-bundle/
├── index.md                                    # Root index (declares okf_version: "0.2")
├── enums/acme/orders/v1/
│   ├── index.md
│   └── OrderStatus.md                          # type: Protobuf Enum
├── messages/acme/orders/v1/
│   ├── index.md
│   ├── GetOrderRequest.md                      # type: Protobuf Message
│   ├── LineItem.md                             # type: Protobuf Message
│   └── Order.md                                # Links to OrderStatus.md and LineItem.md
└── services/acme/orders/v1/
    ├── index.md
    └── OrderService.md                         # Links to GetOrderRequest.md and Order.md
```

### 2. Export & Import OKF Bundles as Protobuf (`.binpb` / `.json`)

```bash
# Export an on-disk OKF bundle to deterministic binary Protobuf (.binpb) or ProtoJSON (.json)
okf-proto export ./okf-bundle -o bundle.binpb --today 2026-10-17
okf-proto export ./okf-bundle -o bundle.json  --today 2026-10-17

# Rehydrate an OKF directory bundle from .binpb or .json
okf-proto import bundle.binpb -o ./restored-bundle
diff -ru ./okf-bundle ./restored-bundle   # byte-identical
```

---

## Go SDK

`okf-proto` is designed to be imported as a pure Go library with minimal dependencies (`google.golang.org/protobuf` and `gopkg.in/yaml.v3`):

- `github.com/ghchinoy/okf-proto/gen/go/okf/v1` (`okfv1pb`) — generated Protobuf types (`Bundle`, `Concept`, `ReservedFile`, `Actorstamp`, `Source`, `DateRange`, `DerivedTrust`, `Link`).
- `github.com/ghchinoy/okf-proto/pkg/bundle` — `Export`, `Import`, `SerializeConcept`, `MarshalBinary`, `UnmarshalBinary`, `MarshalJSON`, `UnmarshalJSON`.
- `github.com/ghchinoy/okf-proto/pkg/descriptor` — `FromDescriptorSet` (`*descriptorpb.FileDescriptorSet` → `*okfv1pb.Bundle`).

---

## License

Licensed under the [Apache License 2.0](LICENSE).
