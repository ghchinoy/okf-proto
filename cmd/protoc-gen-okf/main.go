// Copyright 2026 G. Hussain Chinoy
// SPDX-License-Identifier: Apache-2.0

// Command protoc-gen-okf is a standard protoc / `buf generate` plugin that
// reads a google.protobuf.compiler.CodeGeneratorRequest from stdin and emits a
// complete, conformant Open Knowledge Format (OKF) v0.2 bundle (concepts +
// directory index.md files) in a CodeGeneratorResponse on stdout.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/ghchinoy/okf-proto/pkg/bundle"
	"github.com/ghchinoy/okf-proto/pkg/descriptor"
)

// Version is the canonical protoc-gen-okf version.
var Version = "0.1.0"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v" || os.Args[1] == "version") {
		fmt.Printf("protoc-gen-okf/%s\n", Version)
		return
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(fmt.Sprintf("protoc-gen-okf: reading stdin: %v", err))
		os.Exit(1)
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(raw, req); err != nil {
		writeError(fmt.Sprintf("protoc-gen-okf: unmarshaling CodeGeneratorRequest: %v", err))
		os.Exit(1)
	}

	targets := make(map[string]bool, len(req.GetFileToGenerate()))
	for _, f := range req.GetFileToGenerate() {
		targets[f] = true
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: req.GetProtoFile(),
	}

	b, err := descriptor.FromDescriptorSet(fds, descriptor.Options{
		Producer:    "protoc-gen-okf/" + Version,
		Now:         resolveNow(),
		TargetFiles: targets,
	})
	if err != nil {
		writeError(fmt.Sprintf("protoc-gen-okf: %v", err))
		os.Exit(1)
	}

	supportedFeatures := uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	resp := &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: &supportedFeatures,
	}

	for _, r := range b.GetReserved() {
		name := r.GetRelPath()
		content := r.GetContent()
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    &name,
			Content: &content,
		})
	}
	for _, c := range b.GetConcepts() {
		name := c.GetRelPath()
		data, err := bundle.SerializeConcept(c)
		if err != nil {
			writeError(fmt.Sprintf("protoc-gen-okf: serializing %s: %v", name, err))
			os.Exit(1)
		}
		content := string(data)
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    &name,
			Content: &content,
		})
	}

	out, err := proto.Marshal(resp)
	if err != nil {
		writeError(fmt.Sprintf("protoc-gen-okf: marshaling response: %v", err))
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(out)
}

func writeError(msg string) {
	resp := &pluginpb.CodeGeneratorResponse{Error: &msg}
	if out, err := proto.Marshal(resp); err == nil {
		_, _ = os.Stdout.Write(out)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

func resolveNow() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC()
		}
	}
	return time.Now().UTC()
}
