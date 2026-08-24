// Package generate builds deterministic OKF bundles from locally available Go source.
package generate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mark-ht/okf-ok/internal/lint"
)

const Schema = "okfok.plan/v1"

type Symbol struct {
	Package  string `json:"package"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Receiver string `json:"receiver,omitempty"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	EndLine  int    `json:"end_line"`
	Synopsis string `json:"synopsis,omitempty"`
}

func (s Symbol) ID() string {
	if s.Receiver != "" {
		return s.Package + "." + s.Receiver + "." + s.Name
	}
	return s.Package + "." + s.Name
}

type Document struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Source     string `json:"source,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`
}

type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type Plan struct {
	Schema        string      `json:"schema"`
	Repository    string      `json:"repository"`
	Bundle        string      `json:"bundle"`
	InventoryHash string      `json:"inventory_hash"`
	Documents     []Document  `json:"documents"`
	Symbols       []Symbol    `json:"symbols"`
	Exclusions    []Exclusion `json:"exclusions,omitempty"`
}

type Manifest struct {
	Schema        string   `json:"schema"`
	PlanHash      string   `json:"plan_hash"`
	InventoryHash string   `json:"inventory_hash"`
	Documents     []string `json:"documents"`
}

func Build(ctx context.Context, repository, bundle string) (Plan, error) {
	repo, err := cleanDirectory(repository)
	if err != nil {
		return Plan{}, err
	}
	if bundle == "" {
		bundle = "knowledge"
	}
	if filepath.IsAbs(bundle) {
		return Plan{}, fmt.Errorf("bundle must be repository-relative")
	}
	bundle = filepath.ToSlash(filepath.Clean(bundle))
	if bundle == "." || bundle == ".." || strings.HasPrefix(bundle, "../") {
		return Plan{}, fmt.Errorf("bundle must be below the repository root")
	}
	module, err := modulePath(repo)
	if err != nil {
		return Plan{}, err
	}
	symbols, exclusions, hashes, err := inventory(ctx, repo, bundle)
	if err != nil {
		return Plan{}, err
	}
	sort.Slice(symbols, func(i, j int) bool { return symbolLess(symbols[i], symbols[j]) })
	plan := Plan{Schema: Schema, Repository: repo, Bundle: bundle, Symbols: symbols, Exclusions: exclusions, InventoryHash: hashStrings(hashes)}
	plan.Documents = render(module, bundle, symbols, hashes)
	return plan, nil
}

func cleanDirectory(value string) (string, error) {
	info, err := os.Lstat(value)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("repository root must be a non-symlinked directory: %s", value)
	}
	return filepath.Abs(value)
}

func modulePath(repo string) (string, error) {
	content, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read repository go.mod: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("repository go.mod has no module directive")
}

func inventory(ctx context.Context, repo, bundle string) ([]Symbol, []Exclusion, []string, error) {
	var symbols []Symbol
	var exclusions []Exclusion
	var hashes []string
	err := filepath.WalkDir(repo, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filename == repo {
			return nil
		}
		rel, err := filepath.Rel(repo, filename)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(rel)
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			exclusions = append(exclusions, Exclusion{slash, "symlink"})
			return nil
		}
		if entry.IsDir() {
			if slash == bundle || skipDirectory(filepath.Base(filename)) {
				exclusions = append(exclusions, Exclusion{slash, "excluded directory"})
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(slash, ".go") {
			return nil
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if len(content) > 2<<20 {
			exclusions = append(exclusions, Exclusion{slash, "file exceeds 2 MiB"})
			return nil
		}
		if bytesHasGeneratedHeader(content) {
			exclusions = append(exclusions, Exclusion{slash, "generated Go source"})
			return nil
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filename, content, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", slash, err)
		}
		hash := sha(content)
		hashes = append(hashes, slash+":"+hash)
		symbols = append(symbols, fileSymbols(fileSet, file, slash)...)
		return nil
	})
	return symbols, exclusions, hashes, err
}

func skipDirectory(base string) bool {
	switch base {
	case ".git", ".okfok", "vendor", "node_modules", "dist", "build", "coverage":
		return true
	}
	return false
}
func bytesHasGeneratedHeader(content []byte) bool {
	return strings.Contains(string(content[:min(len(content), 1024)]), "Code generated")
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func sha(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func hashStrings(values []string) string {
	sort.Strings(values)
	return sha([]byte(strings.Join(values, "\n")))
}

func fileSymbols(fileSet *token.FileSet, file *ast.File, filename string) []Symbol {
	pkg := path.Dir(filename)
	if pkg == "." {
		pkg = file.Name.Name
	} else {
		pkg += " (" + file.Name.Name + ")"
	}
	var out []Symbol
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			kind, receiver := "Go Function", ""
			if node.Recv != nil {
				kind = "Go Method"
				receiver = receiverName(node.Recv)
			}
			out = append(out, makeSymbol(fileSet, pkg, kind, node.Name.Name, receiver, filename, node.Pos(), node.End(), node.Doc))
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, makeSymbol(fileSet, pkg, "Go Type", value.Name.Name, "", filename, value.Pos(), value.End(), value.Doc))
					out = append(out, memberSymbols(fileSet, pkg, value.Name.Name, value.Type, filename)...)
				case *ast.ValueSpec:
					kind := "Go Variable"
					if node.Tok == token.CONST {
						kind = "Go Constant"
					}
					for _, name := range value.Names {
						out = append(out, makeSymbol(fileSet, pkg, kind, name.Name, "", filename, value.Pos(), value.End(), value.Doc))
					}
				}
			}
		}
	}
	return out
}
func memberSymbols(fileSet *token.FileSet, pkg, owner string, expression ast.Expr, filename string) []Symbol {
	var out []Symbol
	switch node := expression.(type) {
	case *ast.StructType:
		// Struct fields are represented in their owning type's source, not as
		// standalone concepts. A document per field overwhelms real codebases
		// without adding a useful navigation boundary.
		return nil
	case *ast.InterfaceType:
		for _, field := range node.Methods.List {
			for _, name := range field.Names {
				out = append(out, makeSymbol(fileSet, pkg, "Go Interface Method", name.Name, owner, filename, field.Pos(), field.End(), field.Doc))
			}
		}
	}
	return out
}
func receiverName(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	switch v := fields.List[0].Type.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		if n, ok := v.X.(*ast.Ident); ok {
			return n.Name
		}
	}
	return "receiver"
}
func makeSymbol(fileSet *token.FileSet, pkg, kind, name, receiver, file string, start, end token.Pos, comment *ast.CommentGroup) Symbol {
	return Symbol{Package: pkg, Kind: kind, Name: name, Receiver: receiver, File: file, Line: fileSet.Position(start).Line, EndLine: fileSet.Position(end).Line, Synopsis: commentText(comment)}
}
func commentText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	return strings.Split(strings.TrimSpace(group.Text()), "\n")[0]
}
func symbolLess(a, b Symbol) bool {
	if a.Package != b.Package {
		return a.Package < b.Package
	}
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.ID() < b.ID()
}

func render(module, bundle string, symbols []Symbol, hashes []string) []Document {
	hashByFile := map[string]string{}
	for _, v := range hashes {
		p := strings.SplitN(v, ":", 2)
		hashByFile[p[0]] = p[1]
	}
	byPackage := map[string][]Symbol{}
	for _, s := range symbols {
		byPackage[s.Package] = append(byPackage[s.Package], s)
	}
	var packages []string
	for pkg := range byPackage {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	var documents []Document
	rootLines := []string{"# Knowledge", ""}
	for _, pkg := range packages {
		rootLines = append(rootLines, "* ["+pkg+"](packages/"+slug(pkg)+"/index.md) - Generated Go package knowledge.")
	}
	documents = append(documents, Document{Path: "index.md", Content: strings.Join(rootLines, "\n") + "\n"})
	for _, pkg := range packages {
		packagePath := "packages/" + slug(pkg)
		members := byPackage[pkg]
		index := []string{"# " + pkg, "", "* [Package](package.md) - Generated documentation for Go package `" + pkg + "`.", "", "# Symbols", ""}
		for _, symbol := range members {
			index = append(index, "* ["+symbol.ID()+"](symbols/"+symbolFile(symbol)+") - "+symbol.Kind+".")
		}
		documents = append(documents, Document{Path: packagePath + "/index.md", Content: strings.Join(index, "\n") + "\n"})
		packageDocPath := packagePath + "/package.md"
		body := []string{"# " + pkg, "", "# Symbols", ""}
		for _, symbol := range members {
			body = append(body, "* ["+symbol.ID()+"](symbols/"+symbolFile(symbol)+")")
		}
		documents = append(documents, Document{Path: packageDocPath, Content: concept("Go Package", pkg, "Go package "+pkg+".", "", strings.Join(body, "\n")+"\n")})
		for _, symbol := range members {
			output := packagePath + "/symbols/" + symbolFile(symbol)
			source := relativeSource(bundle, output, symbol.File)
			text := []string{"# " + symbol.ID(), "", "# Source", "", "`" + symbol.File + ":" + strconv.Itoa(symbol.Line) + "-" + strconv.Itoa(symbol.EndLine) + "`"}
			if symbol.Kind == "Go Type" {
				var methods []Symbol
				for _, candidate := range members {
					if candidate.Kind == "Go Method" && candidate.Receiver == symbol.Name {
						methods = append(methods, candidate)
					}
				}
				if len(methods) > 0 {
					text = append(text, "", "# Methods", "")
					for _, method := range methods {
						text = append(text, "* ["+method.ID()+"]("+relativeDocumentLink(output, packagePath+"/symbols/"+symbolFile(method))+")")
					}
				}
			}
			if symbol.Synopsis != "" {
				text = append(text, "", "# Documentation", "", symbol.Synopsis)
			}
			documents = append(documents, Document{Path: output, Source: symbol.File, SourceHash: hashByFile[symbol.File], Content: concept(symbol.Kind, symbol.ID(), symbol.Kind+" "+symbol.ID()+".", source, strings.Join(text, "\n")+"\n")})
		}
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return documents
}
func concept(kind, title, description, resource, body string) string {
	front := "---\ntype: " + strconv.Quote(kind) + "\ntitle: " + strconv.Quote(title) + "\ndescription: " + strconv.Quote(description) + "\n"
	if resource != "" {
		front += "resource: " + strconv.Quote(resource) + "\nsources:\n  - id: source\n    resource: " + strconv.Quote(resource) + "\n    title: \"Go declaration source\"\n"
	}
	return front + "---\n" + body
}
func relativeDocumentLink(from, to string) string {
	value, err := filepath.Rel(filepath.FromSlash(path.Dir(from)), filepath.FromSlash(to))
	if err != nil {
		return to
	}
	return filepath.ToSlash(value)
}

func relativeSource(bundle, document, source string) string {
	from := filepath.FromSlash(path.Dir(path.Join(bundle, document)))
	value, err := filepath.Rel(from, filepath.FromSlash(source))
	if err != nil {
		return source
	}
	return filepath.ToSlash(value)
}
func slug(value string) string {
	value = strings.ToLower(value)
	var out strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		} else {
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-")
}
func symbolFile(symbol Symbol) string { return slug(symbol.Kind+"-"+symbol.ID()) + ".md" }

func PlanHash(plan Plan) string {
	copy := plan
	copy.Repository = ""
	// Exclusions are reporting metadata. In particular, the generated output
	// root appears as excluded after the first apply and must not stale a plan.
	copy.Exclusions = nil
	encoded, _ := json.Marshal(copy)
	return sha(encoded)
}
func WritePlan(output string, plan Plan) error {
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(encoded, '\n'), 0o644)
}
func ReadPlan(input string) (Plan, error) {
	content, err := os.ReadFile(input)
	if err != nil {
		return Plan{}, err
	}
	var plan Plan
	err = json.Unmarshal(content, &plan)
	if err != nil {
		return Plan{}, err
	}
	if plan.Schema != Schema {
		return Plan{}, fmt.Errorf("unsupported plan schema %q", plan.Schema)
	}
	return plan, nil
}

func outputOwnershipError(output string) error {
	entries, err := os.ReadDir(output)
	if err != nil {
		return fmt.Errorf("cannot inspect existing bundle output %q: %w", output, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("bundle output %q exists but is empty and has no .okfok-manifest.json; remove the empty directory with `rmdir %s` and rerun apply", output, output)
	}
	preview := make([]string, 0, min(5, len(entries)))
	for _, entry := range entries[:min(5, len(entries))] {
		preview = append(preview, entry.Name())
	}
	return fmt.Errorf("bundle output %q exists but is not okfok-owned (no .okfok-manifest.json; contains %s); refusing to overwrite hand-authored content", output, strings.Join(preview, ", "))
}

func Apply(ctx context.Context, plan Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := Build(ctx, plan.Repository, plan.Bundle)
	if err != nil {
		return err
	}
	if PlanHash(current) != PlanHash(plan) {
		return fmt.Errorf("plan is stale; regenerate and review it before apply")
	}
	output := filepath.Join(plan.Repository, filepath.FromSlash(plan.Bundle))
	if info, err := os.Lstat(output); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bundle output must not be a symlink")
		}
		if _, err := os.Stat(filepath.Join(output, ".okfok-manifest.json")); err != nil {
			return outputOwnershipError(output)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".okfok-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, document := range plan.Documents {
		target := filepath.Join(stage, filepath.FromSlash(document.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(document.Content), 0o644); err != nil {
			return err
		}
	}
	result, err := lint.CheckWithSummary(ctx, stage, lint.Options{WorkspaceRoot: plan.Repository})
	if err != nil {
		return err
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == lint.SeverityError {
			return fmt.Errorf("generated bundle failed lint: %s: %s", diagnostic.File, diagnostic.Message)
		}
	}
	owned := make([]string, 0, len(plan.Documents))
	for _, doc := range plan.Documents {
		owned = append(owned, doc.Path)
	}
	manifest := Manifest{Schema: "okfok.manifest/v1", PlanHash: PlanHash(plan), InventoryHash: plan.InventoryHash, Documents: owned}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, ".okfok-manifest.json"), append(content, '\n'), 0o644); err != nil {
		return err
	}
	backup := output + ".okfok-backup"
	_ = os.RemoveAll(backup)
	if _, err := os.Lstat(output); err == nil {
		if err := os.Rename(output, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, output); err != nil {
		if _, backupErr := os.Lstat(backup); backupErr == nil {
			_ = os.Rename(backup, output)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}
