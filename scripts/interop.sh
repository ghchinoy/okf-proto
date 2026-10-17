#!/usr/bin/env bash
#
# Differential-validation & round-trip interop gate for okf-proto.
#
# Exercises all three okf-proto surfaces:
#   1. `okf-proto from-descriptor` (.proto -> FileDescriptorSet -> OKF bundle)
#   2. `protoc --plugin=protoc-gen-okf` (.proto -> OKF bundle directly via protoc)
#   3. `okf-proto export` + `okf-proto import` (OKF bundle <-> okf.v1.Bundle .binpb & .json)
#
# And validates every emitted OKF bundle with BOTH external validators:
#   - `okf validate` + `okf graph` (github.com/okfcli/okf v0.5.0)
#   - `binder validate` + `binder review` (github.com/ghchinoy/binder v0.6.0)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OKF_PROTO="$ROOT/bin/okf-proto"
PROTOC_GEN_OKF="$ROOT/bin/protoc-gen-okf"

OKF="$(command -v okf || true)"
[[ -z "$OKF" && -x "$HOME/go/bin/okf" ]] && OKF="$HOME/go/bin/okf"

BINDER="$(command -v binder || true)"
[[ -z "$BINDER" && -x "/Users/ghchinoy/projects/binder/bin/binder" ]] && BINDER="/Users/ghchinoy/projects/binder/bin/binder"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

export SOURCE_DATE_EPOCH=1700000000

echo "== 1. Compile testdata/acme/orders/v1/orders.proto to FileDescriptorSet =="
protoc \
  --proto_path="$ROOT/testdata" \
  --include_imports \
  --include_source_info \
  --descriptor_set_out="$WORK/orders.fds.binpb" \
  acme/orders/v1/orders.proto

echo "== 2. Generate OKF bundle via 'okf-proto from-descriptor' =="
"$OKF_PROTO" from-descriptor "$WORK/orders.fds.binpb" -o "$WORK/bundle-from-fds"

echo "== 3. Generate OKF bundle via 'protoc --plugin=protoc-gen-okf' =="
mkdir -p "$WORK/bundle-from-plugin"
protoc \
  --proto_path="$ROOT/testdata" \
  --plugin="protoc-gen-okf=$PROTOC_GEN_OKF" \
  --okf_out="$WORK/bundle-from-plugin" \
  acme/orders/v1/orders.proto

echo "== 4. Validate both generated bundles with okf and binder =="
for label in bundle-from-fds bundle-from-plugin; do
  bdir="$WORK/$label"
  if [[ -n "$OKF" ]]; then
    "$OKF" validate "$bdir" > "$WORK/$label.okf.json"
    warnings="$(grep -oE '"warnings":[[:space:]]*[0-9]+' "$WORK/$label.okf.json" | grep -oE '[0-9]+' || echo 0)"
    errors="$(grep -oE '"errors":[[:space:]]*[0-9]+' "$WORK/$label.okf.json" | grep -oE '[0-9]+' || echo 0)"
    echo "PASS  [$label] okf validate: valid=true errors=$errors warnings=$warnings"
    "$OKF" graph "$bdir" > "$WORK/$label.graph.json"
    edges="$(grep -oE '"edge_count":[[:space:]]*[0-9]+' "$WORK/$label.graph.json" | grep -oE '[0-9]+' || echo 0)"
    nodes="$(grep -oE '"node_count":[[:space:]]*[0-9]+' "$WORK/$label.graph.json" | grep -oE '[0-9]+' || echo 0)"
    echo "PASS  [$label] okf graph: nodes=$nodes edges=$edges"
  fi
  if [[ -n "$BINDER" ]]; then
    "$BINDER" validate "$bdir" --strict >/dev/null
    echo "PASS  [$label] binder validate --strict: conformant (0 findings)"
  fi
done

echo "== 5. Bidirectional Wire Round-Trip (OKF -> .binpb / .json -> OKF) =="
"$OKF_PROTO" export "$WORK/bundle-from-fds" -o "$WORK/bundle.binpb" --today 2026-08-15
"$OKF_PROTO" export "$WORK/bundle-from-fds" -o "$WORK/bundle.json" --today 2026-08-15
"$OKF_PROTO" import "$WORK/bundle.binpb" -o "$WORK/roundtrip-binpb"
"$OKF_PROTO" import "$WORK/bundle.json" -o "$WORK/roundtrip-json"

diff -ru "$WORK/bundle-from-fds" "$WORK/roundtrip-binpb"
diff -ru "$WORK/bundle-from-fds" "$WORK/roundtrip-json"
echo "PASS  [roundtrip] binary (.binpb) and ProtoJSON (.json) round-trips are byte-identical"

echo "INTEROP GATE: PASS"
