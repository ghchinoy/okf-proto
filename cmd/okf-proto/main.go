// Copyright 2026 G. Hussain Chinoy
// SPDX-License-Identifier: Apache-2.0

// Command okf-proto is a bidirectional converter between Open Knowledge Format
// (OKF) v0.2 bundles and Protocol Buffers (okf.v1.Bundle wire/JSON payloads and
// compiled FileDescriptorSet schemas from buf or protoc).
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	okfv1pb "github.com/ghchinoy/okf-proto/gen/go/okf/v1"
	"github.com/ghchinoy/okf-proto/pkg/bundle"
	"github.com/ghchinoy/okf-proto/pkg/descriptor"
)

// Version is the canonical okf-proto version (no leading "v").
var Version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return 0
	case "-v", "--version", "version":
		fmt.Printf("okf-proto/%s\n", Version)
		return 0
	case "export":
		return runExport(args[1:])
	case "import":
		return runImport(args[1:])
	case "from-descriptor":
		return runFromDescriptor(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "okf-proto: unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, `okf-proto — Open Knowledge Format (OKF) v0.2 <-> Protocol Buffers / Buf

Usage:
  okf-proto export <bundle-dir> [-o <file>] [--format binpb|json] [--today YYYY-MM-DD]
  okf-proto import <input-file> -o <bundle-dir> [--format auto|binpb|json]
  okf-proto from-descriptor <fds.binpb> -o <bundle-dir> [--file <proto-path>...]
  okf-proto version
`)
}

func runExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	outPath := fs.String("o", "", "output file (.binpb or .json); defaults to stdout for json")
	format := fs.String("format", "", "wire format: binpb or json (inferred from -o extension when omitted)")
	today := fs.String("today", "", "YYYY-MM-DD date used to evaluate DerivedTrust.stale")
	fs.SetOutput(os.Stderr)

	pos, err := parseFlagsMixed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintln(os.Stderr, "usage: okf-proto export <bundle-dir> [-o <file>] [--format binpb|json] [--today YYYY-MM-DD]")
		return 2
	}
	bundleDir := pos[0]

	fmtMode := strings.ToLower(strings.TrimSpace(*format))
	if fmtMode == "" {
		if strings.HasSuffix(strings.ToLower(*outPath), ".binpb") || strings.HasSuffix(strings.ToLower(*outPath), ".pb") {
			fmtMode = "binpb"
		} else {
			fmtMode = "json"
		}
	}
	if fmtMode != "binpb" && fmtMode != "json" {
		fmt.Fprintf(os.Stderr, "okf-proto export: unsupported --format %q (want binpb or json)\n", *format)
		return 2
	}

	evalToday := *today
	if evalToday == "" {
		evalToday = resolveNow().Format("2006-01-02")
	}

	b, err := bundle.Export(bundleDir, bundle.ExportOptions{Today: evalToday})
	if err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto export: %v\n", err)
		return 3
	}

	var data []byte
	if fmtMode == "binpb" {
		data, err = bundle.MarshalBinary(b)
	} else {
		data, err = bundle.MarshalJSON(b)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto export: marshaling: %v\n", err)
		return 3
	}

	if *outPath == "" || *outPath == "-" {
		_, _ = os.Stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto export: writing %s: %v\n", *outPath, err)
		return 3
	}
	return 0
}

func runImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	outDir := fs.String("o", "", "output OKF bundle directory (required)")
	format := fs.String("format", "auto", "input format: auto, binpb, or json")
	fs.SetOutput(os.Stderr)

	pos, err := parseFlagsMixed(fs, args)
	if err != nil || len(pos) != 1 || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: okf-proto import <input-file> -o <bundle-dir> [--format auto|binpb|json]")
		return 2
	}
	inPath := pos[0]
	raw, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto import: reading %s: %v\n", inPath, err)
		return 3
	}

	fmtMode := strings.ToLower(strings.TrimSpace(*format))
	if fmtMode == "auto" || fmtMode == "" {
		if strings.HasSuffix(strings.ToLower(inPath), ".json") || (len(raw) > 0 && raw[0] == '{') {
			fmtMode = "json"
		} else {
			fmtMode = "binpb"
		}
	}

	var pbBundle *okfv1pb.Bundle
	var uerr error
	switch fmtMode {
	case "json":
		pbBundle, uerr = bundle.UnmarshalJSON(raw)
	case "binpb":
		pbBundle, uerr = bundle.UnmarshalBinary(raw)
	default:
		uerr = fmt.Errorf("unsupported format %q (want auto, binpb, or json)", fmtMode)
	}
	if uerr != nil {
		fmt.Fprintf(os.Stderr, "okf-proto import: unmarshaling %s (%s): %v\n", inPath, fmtMode, uerr)
		return 3
	}

	if err := bundle.Import(pbBundle, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto import: %v\n", err)
		return 3
	}
	return 0
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func runFromDescriptor(args []string) int {
	fs := flag.NewFlagSet("from-descriptor", flag.ContinueOnError)
	outDir := fs.String("o", "", "output OKF bundle directory (required)")
	var targetFiles stringSlice
	fs.Var(&targetFiles, "file", "restrict concept emission to specific .proto file path (repeatable)")
	fs.SetOutput(os.Stderr)

	pos, err := parseFlagsMixed(fs, args)
	if err != nil || len(pos) != 1 || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: okf-proto from-descriptor <fds.binpb> -o <bundle-dir> [--file <proto-path>...]")
		return 2
	}

	raw, err := os.ReadFile(pos[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto from-descriptor: reading %s: %v\n", pos[0], err)
		return 3
	}

	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, fds); err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto from-descriptor: invalid FileDescriptorSet %s: %v\n", pos[0], err)
		return 3
	}

	targets := map[string]bool{}
	for _, f := range targetFiles {
		if strings.TrimSpace(f) != "" {
			targets[strings.TrimSpace(f)] = true
		}
	}

	b, err := descriptor.FromDescriptorSet(fds, descriptor.Options{
		Producer:    "okf-proto/" + Version,
		Now:         resolveNow(),
		TargetFiles: targets,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto from-descriptor: %v\n", err)
		return 3
	}

	if err := bundle.Import(b, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "okf-proto from-descriptor: writing bundle: %v\n", err)
		return 3
	}
	return 0
}

func resolveNow() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC()
		}
	}
	return time.Now().UTC()
}

// parseFlagsMixed allows flags to appear before or after positional arguments.
func parseFlagsMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flagArgs = append(flagArgs, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	pos = append(pos, fs.Args()...)
	return pos, nil
}
