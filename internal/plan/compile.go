package plan

import (
	"fmt"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Diagnostic is a compile error with location.
type Diagnostic struct {
	Line    int
	Message string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("line %d: %s", d.Line, d.Message)
}

// InvariantDecl is a top-level invariant declaration.
type InvariantDecl struct {
	Name      string
	Statement string
	Evidence  []string
}

// Compile parses an .archon source into a graph containing holes, declared
// boxes, and declared edges. Contract and evidence sections are stored as
// metadata on Invariants. Returns diagnostics on parse errors.
func Compile(src []byte) (*graph.Graph, []Diagnostic) {
	p := &parser{
		lines: strings.Split(string(src), "\n"),
		g:     &graph.Graph{},
		invs:  map[string]*InvariantDecl{},
	}
	p.parse()
	if len(p.diags) > 0 {
		return nil, p.diags
	}
	p.resolveCites()
	if len(p.diags) > 0 {
		return nil, p.diags
	}
	p.g.Sort()
	return p.g, nil
}

type parser struct {
	lines []string
	pos   int
	g     *graph.Graph
	invs  map[string]*InvariantDecl
	diags []Diagnostic

	// per-hole state during parsing
	cites []cite
}

type cite struct {
	holePath string
	invName  string
	line     int
}

func (p *parser) parse() {
	for p.pos < len(p.lines) {
		line := strings.TrimSpace(p.lines[p.pos])
		switch {
		case line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//"):
			p.pos++
		case strings.HasPrefix(line, "hole "):
			p.parseHole()
		case strings.HasPrefix(line, "arrow "):
			p.parseArrow()
		case strings.HasPrefix(line, "box "):
			p.parseBox()
		case strings.HasPrefix(line, "invariant "):
			p.parseInvariant()
		default:
			p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("unexpected: %q", line)})
			p.pos++
		}
	}
}

func (p *parser) parseHole() {
	line := strings.TrimSpace(p.lines[p.pos])
	// hole <path> {
	parts := strings.Fields(line)
	if len(parts) < 3 || parts[len(parts)-1] != "{" {
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: "malformed hole declaration"})
		p.pos++
		return
	}
	path := parts[1]
	p.pos++

	pkg := graph.Package{
		Path:     path,
		Name:     lastSeg(path),
		Internal: true,
		Hole:     true,
	}

	section := ""
	closed := false
	for p.pos < len(p.lines) {
		l := strings.TrimSpace(p.lines[p.pos])
		if l == "}" {
			p.pos++
			closed = true
			break
		}
		switch {
		case l == "surface:" || l == "allow:" || l == "contract:" || l == "evidence:" || l == "cites:":
			section = strings.TrimSuffix(l, ":")
			p.pos++
		case l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//"):
			p.pos++
		default:
			switch section {
			case "surface":
				sym := parseSurfaceEntry(l)
				if sym.Name == "" {
					p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("empty surface entry: %q", l)})
				} else {
					pkg.Surface = append(pkg.Surface, sym)
				}
			case "allow":
				p.parseAllowEntry(l, &pkg)
			case "contract":
				pkg.Invariants = append(pkg.Invariants, parseContractEntry(l))
			case "evidence":
				// PoC: evidence lines are parsed but not stored mechanically.
			case "cites":
				p.parseCiteEntry(l, path)
			default:
				p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("content outside a section: %q", l)})
			}
			p.pos++
		}
	}

	if !closed {
		p.diags = append(p.diags, Diagnostic{Line: p.pos, Message: fmt.Sprintf("unterminated hole block %q (missing closing '}')", path)})
		return
	}
	p.g.Packages = append(p.g.Packages, pkg)
}

func (p *parser) parseAllowEntry(l string, pkg *graph.Package) {
	fields := strings.Fields(l)
	if len(fields) < 2 {
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("malformed allow entry: %q", l)})
		return
	}
	kind := fields[0]
	target := fields[1]

	switch kind {
	case "import":
		pkg.Allow = append(pkg.Allow, target)
	case "metric", "config", "service", "capability", "protocol":
		pkg.Allow = append(pkg.Allow, target)
	default:
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("unknown allow kind: %q", kind)})
	}
}

func (p *parser) parseCiteEntry(l string, holePath string) {
	fields := strings.Fields(l)
	if len(fields) < 2 || fields[0] != "invariant" {
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("malformed cite: %q", l)})
		return
	}
	p.cites = append(p.cites, cite{holePath: holePath, invName: fields[1], line: p.pos + 1})
}

func (p *parser) parseArrow() {
	line := strings.TrimSpace(p.lines[p.pos])
	// arrow <from> -> <to> : <kind>
	parts := strings.Fields(line)
	// Expected: arrow from -> to : kind
	if len(parts) < 6 || parts[2] != "->" || parts[4] != ":" {
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: fmt.Sprintf("malformed arrow: %q", line)})
		p.pos++
		return
	}
	p.g.Edges = append(p.g.Edges, graph.Edge{From: parts[1], To: parts[3], Kind: parts[5]})
	p.pos++
}

func (p *parser) parseBox() {
	line := strings.TrimSpace(p.lines[p.pos])
	parts := strings.Fields(line)
	if len(parts) < 2 {
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: "malformed box declaration"})
		p.pos++
		return
	}
	path := parts[1]
	p.g.Packages = append(p.g.Packages, graph.Package{
		Path:     path,
		Name:     lastSeg(path),
		Internal: true,
	})
	p.pos++
}

func (p *parser) parseInvariant() {
	line := strings.TrimSpace(p.lines[p.pos])
	// invariant <name> {
	parts := strings.Fields(line)
	if len(parts) < 3 || parts[len(parts)-1] != "{" {
		p.diags = append(p.diags, Diagnostic{Line: p.pos + 1, Message: "malformed invariant declaration"})
		p.pos++
		return
	}
	name := parts[1]
	p.pos++

	decl := &InvariantDecl{Name: name}
	closed := false
	for p.pos < len(p.lines) {
		l := strings.TrimSpace(p.lines[p.pos])
		if l == "}" {
			p.pos++
			closed = true
			break
		}
		switch {
		case strings.HasPrefix(l, "statement:"):
			decl.Statement = strings.TrimSpace(strings.TrimPrefix(l, "statement:"))
		case strings.HasPrefix(l, "evidence:"):
			ev := strings.TrimSpace(strings.TrimPrefix(l, "evidence:"))
			for _, e := range strings.Split(ev, ",") {
				decl.Evidence = append(decl.Evidence, strings.TrimSpace(e))
			}
		}
		p.pos++
	}
	if !closed {
		p.diags = append(p.diags, Diagnostic{Line: p.pos, Message: fmt.Sprintf("unterminated invariant block %q (missing closing '}')", name)})
		return
	}
	p.invs[name] = decl
}

func (p *parser) resolveCites() {
	for _, c := range p.cites {
		if _, ok := p.invs[c.invName]; !ok {
			p.diags = append(p.diags, Diagnostic{
				Line:    c.line,
				Message: fmt.Sprintf("undeclared invariant %q cited by %s", c.invName, c.holePath),
			})
		}
	}
}

func parseSurfaceEntry(l string) graph.Symbol {
	// Parse: Name(args) -> ReturnType  or just  Name Type
	name := l
	sig := ""
	if idx := strings.IndexByte(l, '('); idx >= 0 {
		name = l[:idx]
		sig = l[idx:]
		return graph.Symbol{Kind: "func", Name: name, Sig: sig}
	}
	fields := strings.Fields(l)
	if len(fields) >= 2 {
		return graph.Symbol{Kind: "type", Name: fields[0], Sig: strings.Join(fields[1:], " ")}
	}
	return graph.Symbol{Kind: "type", Name: name}
}

func parseContractEntry(l string) graph.Invariant {
	// BC-XX prose [class: detail]
	name := l
	if idx := strings.IndexByte(l, ' '); idx >= 0 {
		name = l[:idx]
	}
	return graph.Invariant{Name: name, File: "plan"}
}

func lastSeg(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

