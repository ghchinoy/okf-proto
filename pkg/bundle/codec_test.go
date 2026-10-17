// Copyright 2026 G. Hussain Chinoy
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExportImportRoundTripByteFaithful(t *testing.T) {
	srcDir := t.TempDir()

	rootIndex := "---\nokf_version: \"0.2\"\n---\n\n# Acme Bundle\n\n- [Orders](tables/orders.md)\n"
	ordersConcept := "---\n" +
		"type: BigQuery Table\n" +
		"title: Customer Orders\n" +
		"description: One row per order.\n" +
		"resource: bigquery://acme.sales.orders\n" +
		"tags:\n" +
		"  - sales\n" +
		"  - orders\n" +
		"status: stable\n" +
		"stale_after: 2026-12-31T00:00:00Z\n" +
		"generated: { by: binder/0.6.0, at: 2026-06-30T14:00:00Z }\n" +
		"verified:\n" +
		"  - { by: human:alice@acme, at: 2026-07-01T09:00:00Z }\n" +
		"sources:\n" +
		"  - id: s1\n" +
		"    resource: https://wiki.acme/orders\n" +
		"    title: Orders Wiki\n" +
		"    author: team:data-platform\n" +
		"    usage_count: \"5000\"\n" +
		"    last_modified: 2026-06-15T00:00:00Z\n" +
		"usage_window: { from: 2026-06-01T00:00:00Z, to: 2026-06-30T00:00:00Z }\n" +
		"custom_extension: preserved-value\n" +
		"---\n\n" +
		"# Customer Orders\n\n" +
		"Links to [Revenue Metric](/metrics/revenue.md) [^s1].\n\n" +
		"[^s1]: Orders Wiki.\n"

	revConcept := "---\n" +
		"type: Metric\n" +
		"title: Revenue\n" +
		"---\n\n" +
		"# Revenue\n\n" +
		"Derived from [Customer Orders](../tables/orders.md).\n"

	writeFile(t, srcDir, "index.md", rootIndex)
	writeFile(t, srcDir, "tables/orders.md", ordersConcept)
	writeFile(t, srcDir, "metrics/revenue.md", revConcept)

	// Export to Protobuf.
	b, err := Export(srcDir, ExportOptions{Today: "2026-08-01"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if b.GetOkfVersion() != "0.2" {
		t.Errorf("OkfVersion = %q, want 0.2", b.GetOkfVersion())
	}
	if len(b.GetConcepts()) != 2 {
		t.Fatalf("len(Concepts) = %d, want 2", len(b.GetConcepts()))
	}
	if len(b.GetReserved()) != 1 {
		t.Fatalf("len(Reserved) = %d, want 1", len(b.GetReserved()))
	}

	// Verify orders concept fields (concepts sorted by ID: metrics/revenue, tables/orders).
	orders := b.GetConcepts()[1]
	if orders.GetId() != "tables/orders" {
		t.Fatalf("orders.Id = %q, want tables/orders", orders.GetId())
	}
	if orders.GetDerived().GetTier() != "human-reviewed" {
		t.Errorf("orders tier = %q, want human-reviewed", orders.GetDerived().GetTier())
	}
	if orders.GetDerived().GetStale() {
		t.Error("orders should not be stale on 2026-08-01")
	}
	if len(orders.GetLinks()) != 1 || !orders.GetLinks()[0].GetResolved() || orders.GetLinks()[0].GetTargetId() != "metrics/revenue" {
		t.Errorf("orders links = %+v, want resolved link to metrics/revenue", orders.GetLinks())
	}
	if got := orders.GetFrontmatter().AsMap()["custom_extension"]; got != "preserved-value" {
		t.Errorf("custom_extension = %v, want preserved-value", got)
	}

	// Round-trip through binary wire format.
	binBytes, err := MarshalBinary(b)
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	fromBin, err := UnmarshalBinary(binBytes)
	if err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	outBinDir := t.TempDir()
	if err := Import(fromBin, outBinDir); err != nil {
		t.Fatalf("Import (bin): %v", err)
	}
	gotOrders, err := os.ReadFile(filepath.Join(outBinDir, "tables/orders.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotOrders) != ordersConcept {
		t.Errorf("binary round-trip not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", gotOrders, ordersConcept)
	}

	// Round-trip through ProtoJSON format with OriginalFrontmatter cleared so
	// deterministic re-encoding from Struct + FrontmatterKeyOrder is exercised too.
	jsonBytes, err := MarshalJSON(b)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	fromJSON, err := UnmarshalJSON(jsonBytes)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	for _, c := range fromJSON.GetConcepts() {
		c.OriginalFrontmatter = nil
	}
	outJSONDir := t.TempDir()
	if err := Import(fromJSON, outJSONDir); err != nil {
		t.Fatalf("Import (json): %v", err)
	}
	reExported, err := Export(outJSONDir, ExportOptions{Today: "2027-01-01"})
	if err != nil {
		t.Fatalf("re-Export: %v", err)
	}
	reOrders := reExported.GetConcepts()[1]
	if !reflect.DeepEqual(reOrders.GetFrontmatterKeyOrder(), orders.GetFrontmatterKeyOrder()) {
		t.Errorf("key order after ProtoJSON re-encode = %v, want %v",
			reOrders.GetFrontmatterKeyOrder(), orders.GetFrontmatterKeyOrder())
	}
	if !reOrders.GetDerived().GetStale() {
		t.Error("re-exported orders should be stale on 2027-01-01 (ISO 8601 datetime stale_after)")
	}
}

func TestCyclicAliasRejectedCleanly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cyclic.md", "---\ntype: Note\na: &x\n  b: *x\n---\n\n# Cyclic\n")
	_, err := Export(dir, ExportOptions{})
	if err == nil || !strings.Contains(err.Error(), "contains itself") {
		t.Fatalf("expected cyclic alias error, got: %v", err)
	}
}

func TestDeterministicBinaryMarshal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "---\ntype: Note\ntitle: A\ncustom: 1\n---\n\n# A\n")
	b, err := Export(dir, ExportOptions{Today: "2026-08-01"})
	if err != nil {
		t.Fatal(err)
	}
	b1, err := MarshalBinary(b)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := MarshalBinary(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("MarshalBinary not deterministic across runs")
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
