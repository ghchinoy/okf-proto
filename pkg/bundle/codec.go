// Copyright 2026 G. Hussain Chinoy
// SPDX-License-Identifier: Apache-2.0

// Package bundle implements bidirectional conversion between on-disk Open
// Knowledge Format (OKF) v0.2 directory bundles and the canonical okf.v1.Bundle
// Protobuf schema (binary wire format and canonical ProtoJSON).
package bundle

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"

	okfv1pb "github.com/ghchinoy/okf-proto/gen/go/okf/v1"
)

// DefaultOKFVersion is the default OKF spec version when none is declared in
// the root index.md (spec §12).
const DefaultOKFVersion = "0.2"

// ExportOptions configures exporting an on-disk OKF bundle to Protobuf.
type ExportOptions struct {
	// Today is the YYYY-MM-DD date used to evaluate DerivedTrust.stale (spec §5.5).
	// When empty, staleness is evaluated only if a caller supplies Today.
	Today string
}

// Export reads the OKF bundle directory at root and returns a populated,
// deterministically ordered *okfv1pb.Bundle message.
func Export(root string, opts ExportOptions) (*okfv1pb.Bundle, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("okf-proto: bundle path %q is not a directory", root)
	}

	var relFiles []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relFiles = append(relFiles, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(relFiles)

	out := &okfv1pb.Bundle{
		OkfVersion: DefaultOKFVersion,
	}

	// Track known concept and reserved IDs for link resolution.
	knownIDs := make(map[string]bool, len(relFiles))
	for _, rel := range relFiles {
		id := strings.TrimSuffix(rel, ".md")
		knownIDs[id] = true
	}

	for _, rel := range relFiles {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if isReservedFile(rel) {
			out.Reserved = append(out.Reserved, &okfv1pb.ReservedFile{
				RelPath: rel,
				Content: string(raw),
			})
			if rel == "index.md" {
				if ver := parseRootOKFVersion(raw); ver != "" {
					out.OkfVersion = ver
				}
			}
			continue
		}

		c, err := parseConcept(rel, raw, opts.Today, knownIDs)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		out.Concepts = append(out.Concepts, c)
	}

	sort.Slice(out.Concepts, func(i, j int) bool {
		return out.Concepts[i].GetId() < out.Concepts[j].GetId()
	})
	sort.Slice(out.Reserved, func(i, j int) bool {
		return out.Reserved[i].GetRelPath() < out.Reserved[j].GetRelPath()
	})

	return out, nil
}

// Import writes an *okfv1pb.Bundle to an on-disk OKF v0.2 bundle directory at outDir.
// If a Concept carries unmodified OriginalFrontmatter bytes, those bytes are
// re-emitted verbatim; otherwise the frontmatter is deterministically encoded
// from Frontmatter in FrontmatterKeyOrder.
func Import(b *okfv1pb.Bundle, outDir string) error {
	if b == nil {
		return fmt.Errorf("okf-proto: nil bundle")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, r := range b.GetReserved() {
		rel := cleanRelPath(r.GetRelPath())
		if rel == "" {
			return fmt.Errorf("okf-proto: invalid reserved file path %q", r.GetRelPath())
		}
		target := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(r.GetContent()), 0o644); err != nil {
			return err
		}
	}

	for _, c := range b.GetConcepts() {
		rel := c.GetRelPath()
		if rel == "" && c.GetId() != "" {
			rel = c.GetId() + ".md"
		}
		rel = cleanRelPath(rel)
		if rel == "" {
			return fmt.Errorf("okf-proto: invalid concept path for id %q", c.GetId())
		}
		data, err := SerializeConcept(c)
		if err != nil {
			return fmt.Errorf("serialize %s: %w", rel, err)
		}
		target := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}

	return nil
}

// SerializeConcept renders a single *okfv1pb.Concept to OKF markdown bytes.
func SerializeConcept(c *okfv1pb.Concept) ([]byte, error) {
	var fmBytes []byte
	var err error
	if len(c.GetOriginalFrontmatter()) > 0 {
		fmBytes = c.GetOriginalFrontmatter()
	} else {
		fmBytes, err = encodeConceptFrontmatter(c)
		if err != nil {
			return nil, err
		}
	}

	body := strings.ReplaceAll(c.GetBody(), "\r\n", "\n")
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	if len(fmBytes) > 0 && !bytes.HasSuffix(fmBytes, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteString("---\n")
	if len(c.GetOriginalFrontmatter()) == 0 && body != "" && !strings.HasPrefix(body, "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString(body)
	return buf.Bytes(), nil
}

// MarshalBinary serializes b to deterministic Protobuf wire bytes.
func MarshalBinary(b *okfv1pb.Bundle) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(b)
}

// UnmarshalBinary parses Protobuf wire bytes into an *okfv1pb.Bundle.
func UnmarshalBinary(data []byte) (*okfv1pb.Bundle, error) {
	b := &okfv1pb.Bundle{}
	if err := proto.Unmarshal(data, b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalJSON serializes b to formatted ProtoJSON bytes.
func MarshalJSON(b *okfv1pb.Bundle) ([]byte, error) {
	raw, err := (protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}).Marshal(b)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// UnmarshalJSON parses ProtoJSON bytes into an *okfv1pb.Bundle.
func UnmarshalJSON(data []byte) (*okfv1pb.Bundle, error) {
	b := &okfv1pb.Bundle{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, b); err != nil {
		return nil, err
	}
	return b, nil
}

func isReservedFile(rel string) bool {
	base := path.Base(strings.TrimSpace(rel))
	return base == "index.md" || base == "log.md"
}

func cleanRelPath(rel string) string {
	rel = path.Clean(strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/"))
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.HasPrefix(rel, "/") {
		return ""
	}
	return rel
}

func parseRootOKFVersion(raw []byte) string {
	fmText, _, err := splitFrontmatter(string(raw))
	if err != nil {
		return ""
	}
	_, vals, err := parseYAMLMapping(fmText)
	if err != nil {
		return ""
	}
	return asString(vals["okf_version"])
}

func parseConcept(rel string, raw []byte, today string, knownIDs map[string]bool) (*okfv1pb.Concept, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\uFEFF")
	fmText, body, err := splitFrontmatter(text)
	if err != nil {
		return nil, err
	}
	fmText = strings.ReplaceAll(fmText, "\r", "\n")
	keyOrder, vals, err := parseYAMLMapping(fmText)
	if err != nil {
		return nil, err
	}

	typ := strings.TrimSpace(asString(vals["type"]))
	if typ == "" {
		return nil, fmt.Errorf("missing required non-empty 'type' (OKF §11.2)")
	}

	id := strings.TrimSuffix(rel, ".md")
	st, err := structpb.NewStruct(vals)
	if err != nil {
		return nil, fmt.Errorf("converting frontmatter to protobuf Struct: %w", err)
	}

	c := &okfv1pb.Concept{
		Id:                  id,
		RelPath:             rel,
		Type:                typ,
		Title:               asString(vals["title"]),
		Description:         asString(vals["description"]),
		Resource:            asString(vals["resource"]),
		Tags:                projectTags(vals["tags"]),
		Sources:             projectSources(vals["sources"]),
		UsageWindow:         projectUsageWindow(vals["usage_window"]),
		Generated:           projectGenerated(vals["generated"]),
		Verified:            projectVerified(vals["verified"]),
		Status:              asString(vals["status"]),
		StaleAfter:          asString(vals["stale_after"]),
		Links:               extractLinks(id, body, knownIDs),
		Body:                body,
		Frontmatter:         st,
		FrontmatterKeyOrder: keyOrder,
		OriginalFrontmatter: []byte(fmText),
	}
	c.Derived = deriveTrust(c, today)
	return c, nil
}

func splitFrontmatter(text string) (fmText, body string, err error) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return "", "", fmt.Errorf("missing frontmatter: document does not start with '---'")
	}
	offset := len(lines[0])
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			end := offset + len(lines[i])
			return text[len(lines[0]):offset], text[end:], nil
		}
		offset += len(lines[i])
	}
	return "", "", fmt.Errorf("invalid frontmatter: unterminated '---' block")
}

func parseYAMLMapping(fmText string) ([]string, map[string]any, error) {
	if strings.TrimSpace(fmText) == "" {
		return nil, map[string]any{}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fmText), &doc); err != nil {
		return nil, nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, map[string]any{}, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("invalid frontmatter: expected a mapping at the top level")
	}
	var keys []string
	vals := make(map[string]any, len(root.Content)/2)
	seenKeys := make(map[string]bool, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i].Value
		v, err := nodeToValue(root.Content[i+1], nil)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid frontmatter: %w", err)
		}
		if !seenKeys[k] {
			seenKeys[k] = true
			keys = append(keys, k)
		}
		vals[k] = v
	}
	return keys, vals, nil
}

func nodeToValue(n *yaml.Node, seen map[*yaml.Node]bool) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeToValue(n.Content[0], seen)
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			v, err := nodeToValue(n.Content[i+1], seen)
			if err != nil {
				return nil, err
			}
			out[n.Content[i].Value] = v
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeToValue(c, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.AliasNode:
		if n.Alias != nil {
			if seen[n.Alias] {
				return nil, fmt.Errorf("yaml: anchor %q value contains itself", n.Value)
			}
			if seen == nil {
				seen = make(map[*yaml.Node]bool)
			}
			seen[n.Alias] = true
			v, err := nodeToValue(n.Alias, seen)
			delete(seen, n.Alias)
			return v, err
		}
		return nil, nil
	default:
		switch n.Tag {
		case "!!null":
			return nil, nil
		case "!!bool":
			var b bool
			if n.Decode(&b) == nil {
				return b, nil
			}
		case "!!int":
			var i int64
			if n.Decode(&i) == nil {
				return i, nil
			}
		case "!!float":
			var f float64
			if n.Decode(&f) == nil {
				return f, nil
			}
		}
		return n.Value, nil
	}
}

func encodeConceptFrontmatter(c *okfv1pb.Concept) ([]byte, error) {
	vals := map[string]any{}
	if c.GetFrontmatter() != nil {
		vals = c.GetFrontmatter().AsMap()
	}
	// Ensure typed fields are reflected when constructing a Concept directly in Protobuf.
	setIfNonEmpty := func(k, v string) {
		if v != "" {
			vals[k] = v
		}
	}
	setIfNonEmpty("type", c.GetType())
	setIfNonEmpty("title", c.GetTitle())
	setIfNonEmpty("description", c.GetDescription())
	setIfNonEmpty("resource", c.GetResource())
	if len(c.GetTags()) > 0 && vals["tags"] == nil {
		tags := make([]any, len(c.GetTags()))
		for i, t := range c.GetTags() {
			tags[i] = t
		}
		vals["tags"] = tags
	}
	if c.GetGenerated() != nil && vals["generated"] == nil {
		g := map[string]any{"by": c.GetGenerated().GetBy()}
		if c.GetGenerated().GetAt() != "" {
			g["at"] = c.GetGenerated().GetAt()
		}
		vals["generated"] = g
	}
	if len(c.GetVerified()) > 0 && vals["verified"] == nil {
		ver := make([]any, 0, len(c.GetVerified()))
		for _, v := range c.GetVerified() {
			m := map[string]any{"by": v.GetBy()}
			if v.GetAt() != "" {
				m["at"] = v.GetAt()
			}
			ver = append(ver, m)
		}
		vals["verified"] = ver
	}
	if len(c.GetSources()) > 0 && vals["sources"] == nil {
		srcs := make([]any, 0, len(c.GetSources()))
		for _, s := range c.GetSources() {
			m := map[string]any{}
			if s.GetId() != "" {
				m["id"] = s.GetId()
			}
			if s.GetResource() != "" {
				m["resource"] = s.GetResource()
			}
			if s.GetTitle() != "" {
				m["title"] = s.GetTitle()
			}
			if s.GetAuthor() != "" {
				m["author"] = s.GetAuthor()
			}
			if s.GetUsageCount() != "" {
				m["usage_count"] = s.GetUsageCount()
			}
			if s.GetLastModified() != "" {
				m["last_modified"] = s.GetLastModified()
			}
			srcs = append(srcs, m)
		}
		vals["sources"] = srcs
	}
	if c.GetUsageWindow() != nil && vals["usage_window"] == nil {
		vals["usage_window"] = map[string]any{
			"from": c.GetUsageWindow().GetFrom(),
			"to":   c.GetUsageWindow().GetTo(),
		}
	}
	setIfNonEmpty("status", c.GetStatus())
	setIfNonEmpty("stale_after", c.GetStaleAfter())

	// Determine key order: FrontmatterKeyOrder first, then any remaining keys in canonical/sorted order.
	var order []string
	seen := map[string]bool{}
	for _, k := range c.GetFrontmatterKeyOrder() {
		if _, ok := vals[k]; ok && !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	canonical := []string{
		"type", "title", "description", "resource", "tags",
		"status", "stale_after", "generated", "verified", "sources", "usage_window",
	}
	for _, k := range canonical {
		if _, ok := vals[k]; ok && !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	var extra []string
	for k := range vals {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range order {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
		valNode := &yaml.Node{}
		if err := valNode.Encode(vals[k]); err != nil {
			return nil, err
		}
		root.Content = append(root.Content, keyNode, valNode)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func projectTags(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(asString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func projectGenerated(v any) *okfv1pb.Actorstamp {
	m := asMap(v)
	if m == nil {
		return nil
	}
	return &okfv1pb.Actorstamp{
		By: asString(m["by"]),
		At: asString(m["at"]),
	}
}

func projectVerified(v any) []*okfv1pb.Actorstamp {
	switch t := v.(type) {
	case map[string]any:
		return []*okfv1pb.Actorstamp{{By: asString(t["by"]), At: asString(t["at"])}}
	case []any:
		out := make([]*okfv1pb.Actorstamp, 0, len(t))
		for _, item := range t {
			if m := asMap(item); m != nil {
				out = append(out, &okfv1pb.Actorstamp{
					By: asString(m["by"]),
					At: asString(m["at"]),
				})
			}
		}
		return out
	default:
		return nil
	}
}

func projectSources(v any) []*okfv1pb.Source {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]*okfv1pb.Source, 0, len(list))
	for _, item := range list {
		m := asMap(item)
		if m == nil {
			continue
		}
		out = append(out, &okfv1pb.Source{
			Id:           asString(m["id"]),
			Resource:     asString(m["resource"]),
			Title:        asString(m["title"]),
			Author:       asString(m["author"]),
			UsageCount:   asString(m["usage_count"]),
			LastModified: asString(m["last_modified"]),
		})
	}
	return out
}

func projectUsageWindow(v any) *okfv1pb.DateRange {
	m := asMap(v)
	if m == nil {
		return nil
	}
	return &okfv1pb.DateRange{
		From: asString(m["from"]),
		To:   asString(m["to"]),
	}
}

func deriveTrust(c *okfv1pb.Concept, today string) *okfv1pb.DerivedTrust {
	tier := "unverified"
	if len(c.GetVerified()) > 0 {
		tier = "machine-confirmed"
		for _, v := range c.GetVerified() {
			if strings.HasPrefix(v.GetBy(), "human:") {
				tier = "human-reviewed"
				break
			}
		}
	}
	stale := false
	sa := strings.TrimSpace(c.GetStaleAfter())
	if sa != "" && today != "" {
		cmp := sa
		if len(today) == 10 && len(cmp) > 10 && cmp[10] == 'T' {
			cmp = cmp[:10]
		}
		stale = today >= cmp
	}
	return &okfv1pb.DerivedTrust{
		Tier:     tier,
		Stale:    stale,
		Attested: c.GetType() == "Attested Computation",
	}
}

var mdLinkRE = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

func extractLinks(fromID, body string, knownIDs map[string]bool) []*okfv1pb.Link {
	masked := maskCode(body)
	matches := mdLinkRE.FindAllStringSubmatchIndex(masked, -1)
	var out []*okfv1pb.Link
	for _, m := range matches {
		if m[0] > 0 && body[m[0]-1] == '!' {
			continue // skip image links ![alt](src)
		}
		text := body[m[2]:m[3]]
		rawTarget := strings.TrimSpace(body[m[4]:m[5]])
		targetID, ok := resolveLink(fromID, rawTarget)
		resolved := ok && knownIDs[targetID]
		out = append(out, &okfv1pb.Link{
			RawTarget: rawTarget,
			TargetId:  targetID,
			Text:      text,
			Resolved:  resolved,
		})
	}
	return out
}

func resolveLink(fromID, rawTarget string) (string, bool) {
	target := strings.TrimSpace(rawTarget)
	if hash := strings.IndexByte(target, '#'); hash >= 0 {
		target = target[:hash]
	}
	if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || !strings.HasSuffix(target, ".md") {
		return "", false
	}
	fromRel := fromID + ".md"
	var resolved string
	if strings.HasPrefix(target, "/") {
		resolved = path.Clean(target)
	} else {
		resolved = path.Clean(path.Join("/"+path.Dir(fromRel), target))
	}
	id := strings.TrimPrefix(strings.TrimSuffix(resolved, ".md"), "/")
	if id == "" || strings.HasPrefix(id, "..") {
		return "", false
	}
	return id, true
}

func maskCode(body string) string {
	out := []byte(body)
	lines := strings.SplitAfter(body, "\n")
	pos := 0
	fenceChar, fenceLen := byte(0), 0
	for _, line := range lines {
		content := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimLeft(content, " \t")
		if fenceLen > 0 {
			if run := leadingRun(trimmed); run >= fenceLen && trimmed[0] == fenceChar && strings.TrimSpace(trimmed[run:]) == "" {
				fenceChar, fenceLen = 0, 0
			}
			blank(out, pos, pos+len(content))
		} else if run := leadingRun(trimmed); run >= 3 {
			fenceChar, fenceLen = trimmed[0], run
			blank(out, pos, pos+len(content))
		} else {
			maskSpans(out, pos, content)
		}
		pos += len(line)
	}
	return string(out)
}

func leadingRun(s string) int {
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return 0
	}
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return n
}

func maskSpans(out []byte, base int, line string) {
	i := 0
	for i < len(line) {
		open := strings.IndexByte(line[i:], '`')
		if open == -1 {
			return
		}
		start := i + open
		n := 0
		for start+n < len(line) && line[start+n] == '`' {
			n++
		}
		j := start + n
		for j < len(line) {
			k := strings.IndexByte(line[j:], '`')
			if k == -1 {
				j = -1
				break
			}
			j += k
			m := 0
			for j+m < len(line) && line[j+m] == '`' {
				m++
			}
			if m == n {
				break
			}
			j += m
		}
		if j == -1 {
			i = start + n
			continue
		}
		blank(out, base+start, base+j+n)
		i = j + n
	}
}

func blank(out []byte, from, to int) {
	for i := from; i < to && i < len(out); i++ {
		if out[i] != '\n' && out[i] != '\r' {
			out[i] = ' '
		}
	}
}
