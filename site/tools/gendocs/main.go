// Command gendocs renders the Go source documentation (godoc) of the Tusk
// module into a single themed HTML page for the branding site.
//
// It discovers packages with golang.org/x/tools/go/packages, builds a
// *go/doc.Package for each, and renders the doc comments to HTML using the
// standard library go/doc/comment printer — the same machinery that powers
// godoc. The output reuses the site's nav, footer, and styles.css so the
// generated page matches the hand-written pages.
//
// Usage:
//
//	gendocs -module-root ../../.. -out ../../documentation.html
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

func main() {
	moduleRoot := flag.String("module-root", ".", "path to the Go module to document")
	out := flag.String("out", "documentation.html", "output HTML file path")
	serve := flag.String("serve", "", "if set (e.g. :8000), serve the output's directory over HTTP after generating")
	flag.Parse()

	pkgs, err := loadPackages(*moduleRoot)
	if err != nil {
		log.Fatalf("gendocs: loading packages: %v", err)
	}
	if len(pkgs) == 0 {
		log.Fatal("gendocs: no packages found")
	}

	docs := make([]*doc.Package, 0, len(pkgs))
	for _, p := range pkgs {
		d, err := doc.NewFromFiles(p.fset, p.files, p.path)
		if err != nil {
			log.Printf("gendocs: skipping %s: %v", p.path, err)
			continue
		}
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ImportPath < docs[j].ImportPath })

	page := render(docs)
	if err := os.WriteFile(*out, []byte(page), 0o600); err != nil {
		log.Fatalf("gendocs: writing %s: %v", *out, err)
	}
	fmt.Printf("gendocs: wrote %d packages to %s\n", len(docs), *out)

	if *serve != "" {
		dir := filepath.Dir(*out)
		fmt.Printf("gendocs: serving %s at http://localhost%s/ (Ctrl+C to stop)\n", dir, *serve)
		srv := &http.Server{
			Addr:              *serve,
			Handler:           http.FileServer(http.Dir(dir)),
			ReadHeaderTimeout: 10 * time.Second,
		}
		log.Fatal(srv.ListenAndServe())
	}
}

type loadedPkg struct {
	path  string
	fset  *token.FileSet
	files []*ast.File
}

// loadPackages discovers every package in the module rooted at dir, parsing
// each with comments retained so go/doc can extract documentation.
func loadPackages(dir string) ([]loadedPkg, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedFiles | packages.NeedCompiledGoFiles,
		Dir:  dir,
		Fset: fset,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			if src == nil {
				// filename is supplied by go/packages from the module's own
				// source tree, not from user input.
				b, err := os.ReadFile(filepath.Clean(filename)) //nolint:gosec // G304: path from go toolchain
				if err != nil {
					return nil, err
				}
				src = b
			}
			return parser.ParseFile(fset, filename, src, parser.ParseComments)
		},
	}
	loaded, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}

	var out []loadedPkg
	for _, p := range loaded {
		if len(p.Syntax) == 0 {
			continue
		}
		out = append(out, loadedPkg{path: p.PkgPath, fset: fset, files: p.Syntax})
	}
	return out, nil
}

// ---------- rendering ----------

func render(docs []*doc.Package) string {
	var b strings.Builder
	b.WriteString(pageHead)
	b.WriteString(`<main><div class="docs">`)

	// Sidebar package index.
	b.WriteString(`<aside class="docs-nav"><h4>Packages</h4><ul>`)
	for _, d := range docs {
		fmt.Fprintf(&b, `<li><a href="#%s">%s</a></li>`, pkgID(d.ImportPath), html.EscapeString(d.Name))
	}
	b.WriteString(`</ul></aside>`)

	// Content.
	b.WriteString(`<div class="docs-content">`)
	b.WriteString(`<h1>API documentation</h1>`)
	fmt.Fprintf(&b, `<p class="muted">Generated from Go source doc comments — module <code>%s</code>.</p>`,
		html.EscapeString(modulePath(docs)))

	for _, d := range docs {
		renderPackage(&b, d)
	}
	b.WriteString(`</div></div></main>`)
	b.WriteString(pageFoot)
	return b.String()
}

func renderPackage(b *strings.Builder, d *doc.Package) {
	id := pkgID(d.ImportPath)
	b.WriteString(`<section class="pkg">`)
	fmt.Fprintf(b, `<p class="pkg-path">%s</p>`, html.EscapeString(d.ImportPath))
	fmt.Fprintf(b, `<h2 id="%s">package %s</h2>`, id, html.EscapeString(d.Name))

	if strings.TrimSpace(d.Doc) != "" {
		b.Write(d.HTML(d.Doc))
	} else {
		b.WriteString(`<p class="docs-empty">No package overview.</p>`)
	}

	renderValues(b, d, d.Consts, "Constants")
	renderValues(b, d, d.Vars, "Variables")

	if len(d.Funcs) > 0 {
		b.WriteString(`<h3>Functions</h3>`)
		for _, f := range d.Funcs {
			renderFunc(b, d, f)
		}
	}

	for _, t := range d.Types {
		renderType(b, d, t)
	}

	b.WriteString(`</section>`)
}

func renderValues(b *strings.Builder, d *doc.Package, vals []*doc.Value, title string) {
	if len(vals) == 0 {
		return
	}
	b.WriteString("<h3>" + title + "</h3>")
	for _, v := range vals {
		writeDecl(b, v.Decl)
		if strings.TrimSpace(v.Doc) != "" {
			b.Write(d.HTML(v.Doc))
		}
	}
}

func renderFunc(b *strings.Builder, d *doc.Package, f *doc.Func) {
	fmt.Fprintf(b, `<h3 id="%s.%s">func %s</h3>`, pkgID(d.ImportPath), html.EscapeString(f.Name), html.EscapeString(f.Name))
	writeDecl(b, f.Decl)
	if strings.TrimSpace(f.Doc) != "" {
		b.Write(d.HTML(f.Doc))
	}
}

func renderType(b *strings.Builder, d *doc.Package, t *doc.Type) {
	fmt.Fprintf(b, `<h3 id="%s.%s">type %s</h3>`, pkgID(d.ImportPath), html.EscapeString(t.Name), html.EscapeString(t.Name))
	writeDecl(b, t.Decl)
	if strings.TrimSpace(t.Doc) != "" {
		b.Write(d.HTML(t.Doc))
	}
	// Constructors and methods.
	for _, f := range t.Funcs {
		renderFunc(b, d, f)
	}
	for _, m := range t.Methods {
		fmt.Fprintf(b, `<h3 id="%s.%s.%s">func (%s) %s</h3>`,
			pkgID(d.ImportPath), html.EscapeString(t.Name), html.EscapeString(m.Name),
			html.EscapeString(t.Name), html.EscapeString(m.Name))
		writeDecl(b, m.Decl)
		if strings.TrimSpace(m.Doc) != "" {
			b.Write(d.HTML(m.Doc))
		}
	}
}

// writeDecl prints a declaration's signature. Function bodies are dropped so
// only the signature is shown, matching godoc.
func writeDecl(b *strings.Builder, node ast.Node) {
	if fd, ok := node.(*ast.FuncDecl); ok {
		clone := *fd
		clone.Body = nil
		node = &clone
	}
	var buf bytes.Buffer
	fset := token.NewFileSet()
	if err := printer.Fprint(&buf, fset, node); err != nil {
		return
	}
	b.WriteString(`<pre class="decl"><code>`)
	b.WriteString(html.EscapeString(buf.String()))
	b.WriteString(`</code></pre>`)
}

// ---------- helpers ----------

func pkgID(importPath string) string {
	return "pkg-" + strings.NewReplacer("/", "-", ".", "-").Replace(importPath)
}

// modulePath returns the longest common slash-delimited prefix of the given
// packages' import paths — a best-effort display of the module path.
func modulePath(docs []*doc.Package) string {
	if len(docs) == 0 {
		return ""
	}
	prefix := strings.Split(docs[0].ImportPath, "/")
	for _, d := range docs[1:] {
		parts := strings.Split(d.ImportPath, "/")
		n := min(len(parts), len(prefix))
		i := 0
		for i < n && prefix[i] == parts[i] {
			i++
		}
		prefix = prefix[:i]
	}
	return strings.Join(prefix, "/")
}

const pageHead = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Documentation — Tusk</title>
    <meta name="description" content="Auto-generated Go API documentation for Tusk, rendered from source doc comments." />
    <link rel="icon" href="./favicon.svg" type="image/svg+xml" />
    <link rel="stylesheet" href="./styles.css" />
  </head>
  <body>
    <header class="nav">
      <a class="nav-brand" href="./index.html">tusk</a>
      <nav class="nav-links">
        <a href="./index.html">Home</a>
        <a href="./features.html">Features</a>
        <a href="./documentation.html" class="active">Docs</a>
        <a href="https://github.com/Fraser-Isbester/tusk">GitHub&nbsp;↗</a>
      </nav>
    </header>
`

const pageFoot = `
    <footer class="footer">
      <p>Tusk — MIT Licensed · © 2026 Fraser Isbester · docs generated from source</p>
      <p><a href="https://github.com/Fraser-Isbester/tusk">github.com/Fraser-Isbester/tusk</a></p>
    </footer>
  </body>
</html>
`
