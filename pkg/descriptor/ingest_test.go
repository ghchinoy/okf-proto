// Copyright 2026 G. Hussain Chinoy
// SPDX-License-Identifier: Apache-2.0

package descriptor

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/ghchinoy/okf-proto/pkg/bundle"
)

func sampleDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("acme/orders/v1/orders.proto"),
				Package: proto.String("acme.orders.v1"),
				Syntax:  proto.String("proto3"),
				EnumType: []*descriptorpb.EnumDescriptorProto{
					{
						Name: proto.String("OrderStatus"),
						Value: []*descriptorpb.EnumValueDescriptorProto{
							{Name: proto.String("ORDER_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
							{Name: proto.String("ORDER_STATUS_ACTIVE"), Number: proto.Int32(1)},
						},
					},
				},
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("Order"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("order_id"),
								Number: proto.Int32(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
							{
								Name:     proto.String("status"),
								Number:   proto.Int32(2),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
								TypeName: proto.String(".acme.orders.v1.OrderStatus"),
							},
						},
					},
					{
						Name: proto.String("GetOrderRequest"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("order_id"),
								Number: proto.Int32(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
						},
					},
					{
						Name:    proto.String("LegacyOrder"),
						Options: &descriptorpb.MessageOptions{Deprecated: proto.Bool(true)},
					},
				},
				Service: []*descriptorpb.ServiceDescriptorProto{
					{
						Name: proto.String("OrderService"),
						Method: []*descriptorpb.MethodDescriptorProto{
							{
								Name:       proto.String("GetOrder"),
								InputType:  proto.String(".acme.orders.v1.GetOrderRequest"),
								OutputType: proto.String(".acme.orders.v1.Order"),
							},
						},
					},
				},
				SourceCodeInfo: &descriptorpb.SourceCodeInfo{
					Location: []*descriptorpb.SourceCodeInfo_Location{
						{
							Path:            []int32{6, 0}, // OrderService
							LeadingComments: proto.String(" OrderService manages customer purchase orders.\n"),
						},
						{
							Path:            []int32{4, 0}, // Order
							LeadingComments: proto.String(" Order represents a single customer order.\n"),
						},
					},
				},
			},
		},
	}
}

func TestFromDescriptorSetEmitsLinkedOKFBundle(t *testing.T) {
	fixedTime := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	b, err := FromDescriptorSet(sampleDescriptorSet(), Options{
		Producer: "okf-proto/0.1.0",
		Now:      fixedTime,
	})
	if err != nil {
		t.Fatalf("FromDescriptorSet: %v", err)
	}
	if b.GetOkfVersion() != "0.2" {
		t.Errorf("OkfVersion = %q, want 0.2", b.GetOkfVersion())
	}
	// Expect 1 enum + 3 messages + 1 service = 5 concepts.
	if len(b.GetConcepts()) != 5 {
		t.Fatalf("len(Concepts) = %d, want 5", len(b.GetConcepts()))
	}

	byID := map[string]int{}
	for i, c := range b.GetConcepts() {
		byID[c.GetId()] = i
	}
	for _, wantID := range []string{
		"enums/acme/orders/v1/OrderStatus",
		"messages/acme/orders/v1/GetOrderRequest",
		"messages/acme/orders/v1/LegacyOrder",
		"messages/acme/orders/v1/Order",
		"services/acme/orders/v1/OrderService",
	} {
		if _, ok := byID[wantID]; !ok {
			t.Errorf("missing expected concept %q; got %v", wantID, byID)
		}
	}

	// Check service concept links to GetOrderRequest and Order.
	svc := b.GetConcepts()[byID["services/acme/orders/v1/OrderService"]]
	if svc.GetType() != TypeProtobufService {
		t.Errorf("svc.Type = %q, want %q", svc.GetType(), TypeProtobufService)
	}
	if svc.GetDescription() != "OrderService manages customer purchase orders." {
		t.Errorf("svc.Description = %q", svc.GetDescription())
	}
	if len(svc.GetLinks()) != 2 {
		t.Fatalf("svc.Links = %+v, want 2 links (request + response)", svc.GetLinks())
	}

	// Check Order message links to OrderStatus enum.
	order := b.GetConcepts()[byID["messages/acme/orders/v1/Order"]]
	if len(order.GetLinks()) != 1 || order.GetLinks()[0].GetTargetId() != "enums/acme/orders/v1/OrderStatus" {
		t.Errorf("order.Links = %+v, want link to enums/acme/orders/v1/OrderStatus", order.GetLinks())
	}

	// Check deprecated option maps to status: deprecated (OKF §5.4).
	legacy := b.GetConcepts()[byID["messages/acme/orders/v1/LegacyOrder"]]
	if legacy.GetStatus() != "deprecated" {
		t.Errorf("legacy.Status = %q, want deprecated", legacy.GetStatus())
	}

	// Materialize to disk and re-export to verify every file parses cleanly and
	// all links resolve on disk.
	outDir := t.TempDir()
	if err := bundle.Import(b, outDir); err != nil {
		t.Fatalf("bundle.Import: %v", err)
	}
	reloaded, err := bundle.Export(outDir, bundle.ExportOptions{Today: "2026-08-15"})
	if err != nil {
		t.Fatalf("bundle.Export over generated bundle: %v", err)
	}
	if len(reloaded.GetConcepts()) != 5 {
		t.Errorf("reloaded concepts = %d, want 5", len(reloaded.GetConcepts()))
	}
	for _, c := range reloaded.GetConcepts() {
		for _, l := range c.GetLinks() {
			if strings.HasPrefix(l.GetRawTarget(), "/") && !l.GetResolved() {
				t.Errorf("concept %s has unresolved internal link %+v", c.GetId(), l)
			}
		}
	}
}
