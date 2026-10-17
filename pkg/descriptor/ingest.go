// Copyright 2026 G. Hussain Chinoy
// SPDX-License-Identifier: Apache-2.0

// Package descriptor converts a compiled Protobuf FileDescriptorSet (from
// `buf build -o -#format=binpb` or `protoc --descriptor_set_out=... --include_source_info`)
// into a conformant Open Knowledge Format (OKF) v0.2 bundle (*okfv1pb.Bundle).
package descriptor

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/structpb"

	okfv1pb "github.com/ghchinoy/okf-proto/gen/go/okf/v1"
)

// Concept type constants emitted by the Protobuf descriptor importer.
const (
	TypeProtobufService = "Protobuf Service"
	TypeProtobufMessage = "Protobuf Message"
	TypeProtobufEnum    = "Protobuf Enum"
)

// Options configures how a FileDescriptorSet is projected into an OKF v0.2 bundle.
type Options struct {
	// Producer is the actor string stamped into generated.by (e.g. "okf-proto/0.1.0").
	// Defaults to "okf-proto/0.1.0".
	Producer string

	// Now is the generation timestamp for generated.at (RFC3339 UTC).
	// Defaults to time.Now().UTC().
	Now time.Time

	// TargetFiles, if non-empty, restricts concept emission to the named .proto
	// files (e.g. files_to_generate in a CodeGeneratorRequest), while still using
	// the full FileDescriptorSet to resolve cross-file type links.
	TargetFiles map[string]bool

	// IncludeWellKnown controls whether google.protobuf.* well-known types are
	// emitted as concepts when TargetFiles is empty. False by default.
	IncludeWellKnown bool
}

// FromDescriptorSet converts fds into a complete, deterministic *okfv1pb.Bundle
// including concept documents for every Service, Message, and Enum, cross-link
// edges between RPCs/fields and their referenced Message/Enum concepts,
// provenance stamps, and per-directory index.md navigation files.
func FromDescriptorSet(fds *descriptorpb.FileDescriptorSet, opts Options) (*okfv1pb.Bundle, error) {
	if fds == nil {
		return nil, fmt.Errorf("okf-proto: nil FileDescriptorSet")
	}
	producer := strings.TrimSpace(opts.Producer)
	if producer == "" {
		producer = "okf-proto/0.1.0"
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	at := now.Format(time.RFC3339)

	// Pass 1: index all emitted symbols (fully-qualified proto name -> concept ID)
	// so field types and RPC inputs/outputs can link to in-bundle concepts.
	typeIndex := map[string]string{} // ".acme.orders.v1.Order" -> "messages/acme/orders/v1/Order"
	filesToEmit := selectFiles(fds.GetFile(), opts)

	for _, fd := range filesToEmit {
		pkg := fd.GetPackage()
		for _, m := range fd.GetMessageType() {
			indexMessageTypes(pkg, "", m, typeIndex)
		}
		for _, e := range fd.GetEnumType() {
			fqn := "." + joinDot(pkg, e.GetName())
			typeIndex[fqn] = enumConceptID(pkg, e.GetName())
		}
	}

	// Pass 2: emit concepts for Services, Messages, and Enums.
	var concepts []*okfv1pb.Concept
	for _, fd := range filesToEmit {
		comments := buildCommentMap(fd.GetSourceCodeInfo())
		pkg := fd.GetPackage()
		fileDeprecated := fd.GetOptions().GetDeprecated()
		protoPath := fd.GetName()

		// Services (field 6 in FileDescriptorProto)
		for si, svc := range fd.GetService() {
			c, err := buildServiceConcept(fd, svc, []int32{6, int32(si)}, comments, pkg, protoPath, fileDeprecated, producer, at, typeIndex)
			if err != nil {
				return nil, err
			}
			concepts = append(concepts, c)
		}

		// Top-level and nested Messages (field 4 in FileDescriptorProto)
		for mi, msg := range fd.GetMessageType() {
			ms, err := buildMessageConcepts(fd, msg, []int32{4, int32(mi)}, "", comments, pkg, protoPath, fileDeprecated, producer, at, typeIndex)
			if err != nil {
				return nil, err
			}
			concepts = append(concepts, ms...)
		}

		// Top-level Enums (field 5 in FileDescriptorProto)
		for ei, en := range fd.GetEnumType() {
			c, err := buildEnumConcept(en, []int32{5, int32(ei)}, "", comments, pkg, protoPath, fileDeprecated, producer, at)
			if err != nil {
				return nil, err
			}
			concepts = append(concepts, c)
		}
	}

	sort.Slice(concepts, func(i, j int) bool {
		return concepts[i].GetId() < concepts[j].GetId()
	})

	reserved := buildDirectoryIndexes(concepts)

	return &okfv1pb.Bundle{
		OkfVersion: "0.2",
		Concepts:   concepts,
		Reserved:   reserved,
	}, nil
}

func selectFiles(files []*descriptorpb.FileDescriptorProto, opts Options) []*descriptorpb.FileDescriptorProto {
	var out []*descriptorpb.FileDescriptorProto
	for _, fd := range files {
		name := fd.GetName()
		if len(opts.TargetFiles) > 0 {
			if !opts.TargetFiles[name] {
				continue
			}
		} else if !opts.IncludeWellKnown && strings.HasPrefix(name, "google/protobuf/") {
			continue
		}
		out = append(out, fd)
	}
	return out
}

func indexMessageTypes(pkg, parent string, m *descriptorpb.DescriptorProto, idx map[string]string) {
	if m.GetOptions().GetMapEntry() {
		return
	}
	qualName := joinDot(parent, m.GetName())
	fqn := "." + joinDot(pkg, qualName)
	idx[fqn] = messageConceptID(pkg, qualName)

	for _, nested := range m.GetNestedType() {
		indexMessageTypes(pkg, qualName, nested, idx)
	}
	for _, en := range m.GetEnumType() {
		efqn := "." + joinDot(pkg, joinDot(qualName, en.GetName()))
		idx[efqn] = enumConceptID(pkg, joinDot(qualName, en.GetName()))
	}
}

func serviceConceptID(pkg, name string) string {
	return path.Join("services", pkgToSlash(pkg), sanitizeSegment(name))
}

func messageConceptID(pkg, qualName string) string {
	return path.Join("messages", pkgToSlash(pkg), sanitizeSegment(qualName))
}

func enumConceptID(pkg, qualName string) string {
	return path.Join("enums", pkgToSlash(pkg), sanitizeSegment(qualName))
}

func pkgToSlash(pkg string) string {
	if pkg == "" {
		return "default"
	}
	return strings.ReplaceAll(pkg, ".", "/")
}

func sanitizeSegment(s string) string {
	return strings.ReplaceAll(s, ".", "_")
}

func joinDot(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

func buildServiceConcept(
	fd *descriptorpb.FileDescriptorProto,
	svc *descriptorpb.ServiceDescriptorProto,
	locPath []int32,
	comments map[string]string,
	pkg, protoPath string,
	fileDeprecated bool,
	producer, at string,
	typeIndex map[string]string,
) (*okfv1pb.Concept, error) {
	id := serviceConceptID(pkg, svc.GetName())
	fqn := joinDot(pkg, svc.GetName())
	lead := cleanComment(comments[pathKey(locPath)])
	desc := firstSentence(lead, fmt.Sprintf("Protobuf RPC service %s defined in %s.", fqn, protoPath))
	deprecated := fileDeprecated || svc.GetOptions().GetDeprecated()

	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n", svc.GetName())
	if lead != "" {
		fmt.Fprintf(&body, "%s [^1]\n\n", lead)
	} else {
		fmt.Fprintf(&body, "`%s` is a Protobuf service defined in `%s` [^1].\n\n", fqn, protoPath)
	}

	var links []*okfv1pb.Link
	seenLinks := map[string]bool{}
	addLink := func(targetID, text string) string {
		rawTarget := "/" + targetID + ".md"
		key := rawTarget + "|" + text
		if !seenLinks[key] {
			seenLinks[key] = true
			links = append(links, &okfv1pb.Link{
				RawTarget: rawTarget,
				TargetId:  targetID,
				Text:      text,
				Resolved:  true,
			})
		}
		return fmt.Sprintf("[%s](%s)", text, rawTarget)
	}

	if len(svc.GetMethod()) > 0 {
		body.WriteString("## Methods\n\n")
		body.WriteString("| Method | Request | Response | Description |\n")
		body.WriteString("| --- | --- | --- | --- |\n")
		for mi, m := range svc.GetMethod() {
			mComment := cleanComment(comments[pathKey(append(append([]int32(nil), locPath...), 2, int32(mi)))])
			mSummary := singleLine(mComment)
			if m.GetOptions().GetDeprecated() {
				if mSummary != "" {
					mSummary = "**Deprecated.** " + mSummary
				} else {
					mSummary = "**Deprecated.**"
				}
			}
			reqCell := formatTypeRef(m.GetInputType(), m.GetClientStreaming(), typeIndex, addLink)
			respCell := formatTypeRef(m.GetOutputType(), m.GetServerStreaming(), typeIndex, addLink)
			fmt.Fprintf(&body, "| `%s` | %s | %s | %s |\n", m.GetName(), reqCell, respCell, escapeTableCell(mSummary))
		}
		body.WriteString("\n")
	}

	fmt.Fprintf(&body, "[^1]: Derived from Protobuf schema `%s` (package `%s`).\n", protoPath, pkg)

	return makeConcept(conceptSpec{
		id:          id,
		typ:         TypeProtobufService,
		title:       svc.GetName(),
		description: desc,
		resource:    "proto://" + fqn,
		tags:        tagsFor(pkg, "protobuf", "service"),
		deprecated:  deprecated,
		protoPath:   protoPath,
		producer:    producer,
		at:          at,
		links:       links,
		body:        body.String(),
	})
}

func buildMessageConcepts(
	fd *descriptorpb.FileDescriptorProto,
	msg *descriptorpb.DescriptorProto,
	locPath []int32,
	parent string,
	comments map[string]string,
	pkg, protoPath string,
	fileDeprecated bool,
	producer, at string,
	typeIndex map[string]string,
) ([]*okfv1pb.Concept, error) {
	if msg.GetOptions().GetMapEntry() {
		return nil, nil
	}
	qualName := joinDot(parent, msg.GetName())
	id := messageConceptID(pkg, qualName)
	fqn := joinDot(pkg, qualName)
	lead := cleanComment(comments[pathKey(locPath)])
	desc := firstSentence(lead, fmt.Sprintf("Protobuf message %s defined in %s.", fqn, protoPath))
	deprecated := fileDeprecated || msg.GetOptions().GetDeprecated()

	// Collect map entry types so map<K, V> fields render cleanly as map<K, V>.
	mapEntries := map[string]*descriptorpb.DescriptorProto{}
	for _, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			entryFQN := "." + joinDot(fqn, nested.GetName())
			mapEntries[entryFQN] = nested
		}
	}

	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n", qualName)
	if lead != "" {
		fmt.Fprintf(&body, "%s [^1]\n\n", lead)
	} else {
		fmt.Fprintf(&body, "`%s` is a Protobuf message defined in `%s` [^1].\n\n", fqn, protoPath)
	}

	var links []*okfv1pb.Link
	seenLinks := map[string]bool{}
	addLink := func(targetID, text string) string {
		if targetID == id {
			// Self-reference still forms a valid graph edge.
		}
		rawTarget := "/" + targetID + ".md"
		key := rawTarget + "|" + text
		if !seenLinks[key] {
			seenLinks[key] = true
			links = append(links, &okfv1pb.Link{
				RawTarget: rawTarget,
				TargetId:  targetID,
				Text:      text,
				Resolved:  true,
			})
		}
		return fmt.Sprintf("[%s](%s)", text, rawTarget)
	}

	if len(msg.GetField()) > 0 {
		body.WriteString("## Fields\n\n")
		body.WriteString("| # | Field | Label | Type | Description |\n")
		body.WriteString("| --- | --- | --- | --- | --- |\n")
		for fi, f := range msg.GetField() {
			fComment := cleanComment(comments[pathKey(append(append([]int32(nil), locPath...), 2, int32(fi)))])
			fSummary := singleLine(fComment)
			if f.GetOptions().GetDeprecated() {
				if fSummary != "" {
					fSummary = "**Deprecated.** " + fSummary
				} else {
					fSummary = "**Deprecated.**"
				}
			}
			label, typeCell := renderFieldType(f, mapEntries, typeIndex, addLink)
			fmt.Fprintf(&body, "| %d | `%s` | %s | %s | %s |\n",
				f.GetNumber(), f.GetName(), label, typeCell, escapeTableCell(fSummary))
		}
		body.WriteString("\n")
	}

	fmt.Fprintf(&body, "[^1]: Derived from Protobuf schema `%s` (package `%s`).\n", protoPath, pkg)

	c, err := makeConcept(conceptSpec{
		id:          id,
		typ:         TypeProtobufMessage,
		title:       qualName,
		description: desc,
		resource:    "proto://" + fqn,
		tags:        tagsFor(pkg, "protobuf", "message"),
		deprecated:  deprecated,
		protoPath:   protoPath,
		producer:    producer,
		at:          at,
		links:       links,
		body:        body.String(),
	})
	if err != nil {
		return nil, err
	}

	out := []*okfv1pb.Concept{c}

	// Recurse into nested messages (field 3) and nested enums (field 4).
	for ni, nested := range msg.GetNestedType() {
		ns, err := buildMessageConcepts(fd, nested, append(append([]int32(nil), locPath...), 3, int32(ni)), qualName, comments, pkg, protoPath, deprecated, producer, at, typeIndex)
		if err != nil {
			return nil, err
		}
		out = append(out, ns...)
	}
	for ei, en := range msg.GetEnumType() {
		ec, err := buildEnumConcept(en, append(append([]int32(nil), locPath...), 4, int32(ei)), qualName, comments, pkg, protoPath, deprecated, producer, at)
		if err != nil {
			return nil, err
		}
		out = append(out, ec)
	}
	return out, nil
}

func buildEnumConcept(
	en *descriptorpb.EnumDescriptorProto,
	locPath []int32,
	parent string,
	comments map[string]string,
	pkg, protoPath string,
	parentDeprecated bool,
	producer, at string,
) (*okfv1pb.Concept, error) {
	qualName := joinDot(parent, en.GetName())
	id := enumConceptID(pkg, qualName)
	fqn := joinDot(pkg, qualName)
	lead := cleanComment(comments[pathKey(locPath)])
	desc := firstSentence(lead, fmt.Sprintf("Protobuf enum %s defined in %s.", fqn, protoPath))
	deprecated := parentDeprecated || en.GetOptions().GetDeprecated()

	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n", qualName)
	if lead != "" {
		fmt.Fprintf(&body, "%s [^1]\n\n", lead)
	} else {
		fmt.Fprintf(&body, "`%s` is a Protobuf enumeration defined in `%s` [^1].\n\n", fqn, protoPath)
	}

	if len(en.GetValue()) > 0 {
		body.WriteString("## Values\n\n")
		body.WriteString("| Number | Name | Description |\n")
		body.WriteString("| --- | --- | --- |\n")
		for vi, v := range en.GetValue() {
			vComment := cleanComment(comments[pathKey(append(append([]int32(nil), locPath...), 2, int32(vi)))])
			vSummary := singleLine(vComment)
			if v.GetOptions().GetDeprecated() {
				if vSummary != "" {
					vSummary = "**Deprecated.** " + vSummary
				} else {
					vSummary = "**Deprecated.**"
				}
			}
			fmt.Fprintf(&body, "| %d | `%s` | %s |\n", v.GetNumber(), v.GetName(), escapeTableCell(vSummary))
		}
		body.WriteString("\n")
	}

	fmt.Fprintf(&body, "[^1]: Derived from Protobuf schema `%s` (package `%s`).\n", protoPath, pkg)

	return makeConcept(conceptSpec{
		id:          id,
		typ:         TypeProtobufEnum,
		title:       qualName,
		description: desc,
		resource:    "proto://" + fqn,
		tags:        tagsFor(pkg, "protobuf", "enum"),
		deprecated:  deprecated,
		protoPath:   protoPath,
		producer:    producer,
		at:          at,
		body:        body.String(),
	})
}

type conceptSpec struct {
	id          string
	typ         string
	title       string
	description string
	resource    string
	tags        []string
	deprecated  bool
	protoPath   string
	producer    string
	at          string
	links       []*okfv1pb.Link
	body        string
}

func makeConcept(s conceptSpec) (*okfv1pb.Concept, error) {
	status := "stable"
	if s.deprecated {
		status = "deprecated"
	}
	tagVals := make([]any, len(s.tags))
	for i, t := range s.tags {
		tagVals[i] = t
	}
	fmMap := map[string]any{
		"type":        s.typ,
		"title":       s.title,
		"description": s.description,
		"resource":    s.resource,
		"tags":        tagVals,
		"status":      status,
		"generated": map[string]any{
			"by": s.producer,
			"at": s.at,
		},
		"sources": []any{
			map[string]any{
				"id":       "1",
				"resource": s.protoPath,
				"title":    s.protoPath,
			},
		},
	}
	st, err := structpb.NewStruct(fmMap)
	if err != nil {
		return nil, err
	}
	order := []string{
		"type", "title", "description", "resource", "tags", "status", "generated", "sources",
	}
	return &okfv1pb.Concept{
		Id:          s.id,
		RelPath:     s.id + ".md",
		Type:        s.typ,
		Title:       s.title,
		Description: s.description,
		Resource:    s.resource,
		Tags:        s.tags,
		Status:      status,
		Generated: &okfv1pb.Actorstamp{
			By: s.producer,
			At: s.at,
		},
		Sources: []*okfv1pb.Source{
			{
				Id:       "1",
				Resource: s.protoPath,
				Title:    s.protoPath,
			},
		},
		Derived: &okfv1pb.DerivedTrust{
			Tier:     "unverified",
			Stale:    false,
			Attested: false,
		},
		Links:               s.links,
		Body:                s.body,
		Frontmatter:         st,
		FrontmatterKeyOrder: order,
	}, nil
}

func renderFieldType(
	f *descriptorpb.FieldDescriptorProto,
	mapEntries map[string]*descriptorpb.DescriptorProto,
	typeIndex map[string]string,
	addLink func(targetID, text string) string,
) (label, typeCell string) {
	if entry, ok := mapEntries[f.GetTypeName()]; ok && len(entry.GetField()) == 2 {
		_, kType := renderFieldType(entry.GetField()[0], nil, typeIndex, addLink)
		_, vType := renderFieldType(entry.GetField()[1], nil, typeIndex, addLink)
		return "map", fmt.Sprintf("map<%s, %s>", kType, vType)
	}

	switch f.GetLabel() {
	case descriptorpb.FieldDescriptorProto_LABEL_REPEATED:
		label = "repeated"
	case descriptorpb.FieldDescriptorProto_LABEL_REQUIRED:
		label = "required"
	default:
		if f.GetProto3Optional() {
			label = "optional"
		} else {
			label = "singular"
		}
	}

	switch f.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		return label, formatTypeRef(f.GetTypeName(), false, typeIndex, addLink)
	default:
		scalar := strings.ToLower(strings.TrimPrefix(f.GetType().String(), "TYPE_"))
		return label, "`" + scalar + "`"
	}
}

func formatTypeRef(typeName string, streaming bool, typeIndex map[string]string, addLink func(targetID, text string) string) string {
	short := shortTypeName(typeName)
	var ref string
	if targetID, ok := typeIndex[typeName]; ok {
		ref = addLink(targetID, short)
	} else {
		ref = "`" + strings.TrimPrefix(typeName, ".") + "`"
	}
	if streaming {
		return "stream " + ref
	}
	return ref
}

func shortTypeName(fqn string) string {
	fqn = strings.TrimPrefix(fqn, ".")
	if i := strings.LastIndexByte(fqn, '.'); i >= 0 {
		return fqn[i+1:]
	}
	return fqn
}

func buildDirectoryIndexes(concepts []*okfv1pb.Concept) []*okfv1pb.ReservedFile {
	byDir := map[string][]*okfv1pb.Concept{}
	childDirs := map[string]map[string]bool{}

	for _, c := range concepts {
		dir := path.Dir(c.GetRelPath())
		if dir == "." {
			dir = ""
		}
		byDir[dir] = append(byDir[dir], c)

		// Register ancestor directories.
		cur := dir
		for cur != "" {
			parent := path.Dir(cur)
			if parent == "." {
				parent = ""
			}
			if childDirs[parent] == nil {
				childDirs[parent] = map[string]bool{}
			}
			childDirs[parent][path.Base(cur)] = true
			cur = parent
		}
	}

	allDirsSet := map[string]bool{"": true}
	for d := range byDir {
		allDirsSet[d] = true
	}
	for d := range childDirs {
		allDirsSet[d] = true
	}
	var allDirs []string
	for d := range allDirsSet {
		allDirs = append(allDirs, d)
	}
	sort.Strings(allDirs)

	var reserved []*okfv1pb.ReservedFile
	for _, dir := range allDirs {
		var b strings.Builder
		if dir == "" {
			b.WriteString("---\nokf_version: \"0.2\"\n---\n\n# Bundle Index\n\n")
		} else {
			fmt.Fprintf(&bodyBuilder{&b}, "# %s\n\n", path.Base(dir))
		}

		if subs := childDirs[dir]; len(subs) > 0 {
			var subNames []string
			for s := range subs {
				subNames = append(subNames, s)
			}
			sort.Strings(subNames)
			b.WriteString("## Directories\n\n")
			for _, s := range subNames {
				fmt.Fprintf(&b, "- [%s](%s/index.md)\n", s, s)
			}
			b.WriteString("\n")
		}

		if list := byDir[dir]; len(list) > 0 {
			b.WriteString("## Concepts\n\n")
			for _, c := range list {
				base := path.Base(c.GetRelPath())
				fmt.Fprintf(&b, "- [%s](%s) (%s) — %s\n", c.GetTitle(), base, c.GetType(), c.GetDescription())
			}
		}

		rel := "index.md"
		if dir != "" {
			rel = path.Join(dir, "index.md")
		}
		reserved = append(reserved, &okfv1pb.ReservedFile{
			RelPath: rel,
			Content: b.String(),
		})
	}
	sort.Slice(reserved, func(i, j int) bool {
		return reserved[i].GetRelPath() < reserved[j].GetRelPath()
	})
	return reserved
}

type bodyBuilder struct{ *strings.Builder }

func tagsFor(pkg string, base ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, b := range base {
		add(b)
	}
	for _, part := range strings.Split(pkg, ".") {
		add(part)
	}
	return out
}

func buildCommentMap(sci *descriptorpb.SourceCodeInfo) map[string]string {
	out := map[string]string{}
	if sci == nil {
		return out
	}
	for _, loc := range sci.GetLocation() {
		text := strings.TrimSpace(loc.GetLeadingComments())
		if text == "" {
			text = strings.TrimSpace(loc.GetTrailingComments())
		}
		if text != "" {
			out[pathKey(loc.GetPath())] = text
		}
	}
	return out
}

func pathKey(p []int32) string {
	parts := make([]string, len(p))
	for i, v := range p {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, ".")
}

func cleanComment(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i, l := range lines {
		l = strings.TrimPrefix(l, " ")
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func firstSentence(text, fallback string) string {
	text = singleLine(text)
	if text == "" {
		return fallback
	}
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}
	return text
}

func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func escapeTableCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
