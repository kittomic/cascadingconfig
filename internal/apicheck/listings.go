package apicheck

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	fenceOpen  = regexp.MustCompile("^\\s*```(\\S*)\\s*$")
	fenceClose = regexp.MustCompile("^\\s*```\\s*$")
	marker     = regexp.MustCompile(`^\s*// package (\S+(?:, \S+)*)(?: \(.*\))?\s*$`)
	superseded = regexp.MustCompile(`^\s*<!-- api superseded DL-\d+ -->\s*$`)
	example    = regexp.MustCompile(`^\s*<!-- example -->\s*$`)
	iotaWord   = regexp.MustCompile(`\biota\b`)
)

// Listings checks the Go listings in doc, the markdown file called name, against pkgs. module is the import path that
// package markers' paths are relative to, and "." names module itself.
func Listings(module string, pkgs map[string]*types.Package, name string, doc []byte) []Finding {
	c := &checker{module: module, pkgs: pkgs, doc: name}
	lines := strings.Split(string(doc), "\n")
	prev := "" // the last non-blank line before the current one
	for i := 0; i < len(lines); i++ {
		m := fenceOpen.FindStringSubmatch(lines[i])
		if m == nil {
			if strings.TrimSpace(lines[i]) != "" {
				prev = lines[i]
			}
			continue
		}
		start := i
		i++
		for i < len(lines) && !fenceClose.MatchString(lines[i]) {
			i++
		}
		if m[1] == "go" && !superseded.MatchString(prev) && !example.MatchString(prev) {
			c.block(start+1, lines[start+1:min(i, len(lines))])
		}
		prev = ""
	}
	return c.findings
}

type checker struct {
	module   string
	pkgs     map[string]*types.Package
	doc      string
	findings []Finding
}

func (c *checker) add(line int, pkg, name, format string, args ...any) {
	c.findings = append(c.findings, Finding{Doc: c.doc, Line: line, Package: pkg, Name: name,
		Message: fmt.Sprintf(format, args...)})
}

// block checks one listing whose fence is on line fence and whose body is body, splitting it at package markers.
func (c *checker) block(fence int, body []string) {
	targets := []string{c.module}
	segStart, unparsable := 0, false
	flush := func(end int) {
		if targets == nil || unparsable {
			return
		}
		src := "package listing\n" + strings.Join(body[segStart:end], "\n")
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
		if err != nil {
			unparsable = true
			c.add(fence, "", "", "unparsable: %v", err)
			return
		}
		// File line 2 is the segment's first line, which is the doc's line fence+1+segStart.
		offset := fence + segStart - 1
		for _, path := range targets {
			d := &declChecker{checker: c, pkg: c.pkgs[path], path: path, fset: fset, offset: offset}
			for _, decl := range f.Decls {
				d.decl(decl)
			}
		}
	}
	for i, line := range body {
		m := marker.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		flush(i)
		segStart = i + 1
		targets = nil
		var resolved []string
		for _, rel := range strings.Split(m[1], ", ") {
			path := c.module
			if rel != "." {
				path += "/" + rel
			}
			if c.pkgs[path] == nil {
				c.add(fence+1+i, "", "", "unknown package: %s", rel)
				resolved = nil
				break
			}
			resolved = append(resolved, path)
		}
		targets = resolved
	}
	flush(len(body))
}

// declChecker checks one segment's declarations against one package.
type declChecker struct {
	*checker
	pkg    *types.Package
	path   string
	fset   *token.FileSet
	offset int
}

func (d *declChecker) line(pos token.Pos) int { return d.fset.Position(pos).Line + d.offset }

func (d *declChecker) report(pos token.Pos, name, format string, args ...any) {
	d.add(d.line(pos), d.path, name, format, args...)
}

func (d *declChecker) lookup(pos token.Pos, name, want string) types.Object {
	obj := d.pkg.Scope().Lookup(name)
	if obj == nil {
		d.report(pos, name, "missing: %s has no %s", d.path, name)
		return nil
	}
	if got := kindOf(obj); got != want {
		d.report(pos, name, "kind: the doc has a %s, the code a %s", want, got)
		return nil
	}
	return obj
}

func kindOf(obj types.Object) string {
	switch obj.(type) {
	case *types.Func:
		return "func"
	case *types.TypeName:
		return "type"
	case *types.Var:
		return "var"
	case *types.Const:
		return "const"
	}
	return "declaration"
}

func (d *declChecker) differs(pos token.Pos, name, doc, code string) {
	if doc != code {
		d.report(pos, name, "differs: the doc has %s, the code %s", doc, code)
	}
}

func (d *declChecker) decl(decl ast.Decl) {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		if decl.Recv == nil {
			d.function(decl)
		} else {
			d.method(decl)
		}
	case *ast.GenDecl:
		var last *ast.ValueSpec // in a const group, a spec with no values repeats the last one's type and values
		for i, spec := range decl.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				d.typeSpec(spec)
			case *ast.ValueSpec:
				if decl.Tok == token.CONST {
					if len(spec.Values) == 0 && last != nil {
						spec = &ast.ValueSpec{Names: spec.Names, Type: last.Type, Values: last.Values}
					}
					last = spec
				}
				d.valueSpec(decl.Tok, spec, i)
			}
		}
	}
}

func (d *declChecker) function(fn *ast.FuncDecl) {
	if !fn.Name.IsExported() {
		return
	}
	obj := d.lookup(fn.Name.Pos(), fn.Name.Name, "func")
	if obj == nil {
		return
	}
	doc := newSyntax(fn.Type.TypeParams, nil)
	r := renderer{home: d.pkg, rename: true}
	d.differs(fn.Name.Pos(), fn.Name.Name, "func"+doc.signature(fn.Type, true),
		"func"+r.signature(obj.Type().(*types.Signature), true))
}

func (d *declChecker) method(fn *ast.FuncDecl) {
	recv := fn.Recv.List[0].Type
	pointer := false
	if star, ok := recv.(*ast.StarExpr); ok {
		recv, pointer = star.X, true
	}
	var recvParams []*ast.Ident
	switch r := recv.(type) {
	case *ast.IndexExpr:
		recv = r.X
		if id, ok := r.Index.(*ast.Ident); ok {
			recvParams = append(recvParams, id)
		}
	case *ast.IndexListExpr:
		recv = r.X
		for _, ix := range r.Indices {
			if id, ok := ix.(*ast.Ident); ok {
				recvParams = append(recvParams, id)
			}
		}
	}
	id, ok := recv.(*ast.Ident)
	if !ok || !id.IsExported() || !fn.Name.IsExported() {
		return
	}
	name := id.Name + "." + fn.Name.Name
	tn, _ := d.pkg.Scope().Lookup(id.Name).(*types.TypeName)
	if tn == nil {
		d.report(fn.Name.Pos(), name, "missing: %s has no type %s", d.path, id.Name)
		return
	}
	obj, _, _ := types.LookupFieldOrMethod(tn.Type(), true, d.pkg, fn.Name.Name)
	m, ok := obj.(*types.Func)
	if !ok {
		d.report(fn.Name.Pos(), name, "missing: %s has no method %s", id.Name, fn.Name.Name)
		return
	}
	if !pointer && types.NewMethodSet(tn.Type()).Lookup(d.pkg, fn.Name.Name) == nil {
		d.report(fn.Name.Pos(), name, "differs: the doc has a value receiver, the code a pointer one")
		return
	}
	doc := newSyntax(nil, recvParams)
	r := renderer{home: d.pkg, rename: true}
	d.differs(fn.Name.Pos(), name, fn.Name.Name+doc.signature(fn.Type, false),
		fn.Name.Name+r.signature(m.Type().(*types.Signature), false))
}

func (d *declChecker) typeSpec(spec *ast.TypeSpec) {
	if !spec.Name.IsExported() {
		return
	}
	name := spec.Name.Name
	obj := d.lookup(spec.Name.Pos(), name, "type")
	if obj == nil {
		return
	}
	tn := obj.(*types.TypeName)
	doc := newSyntax(spec.TypeParams, nil)
	r := renderer{home: d.pkg, rename: true}
	pos := spec.Name.Pos()
	if (spec.Assign != token.NoPos) != tn.IsAlias() {
		d.report(pos, name, "differs: the doc has %s, the code %s", aliasWord(spec.Assign != token.NoPos),
			aliasWord(tn.IsAlias()))
		return
	}
	if tn.IsAlias() {
		d.differs(pos, name, "= "+doc.typ(spec.Type), "= "+r.typ(types.Unalias(tn.Type())))
		return
	}
	named, _ := tn.Type().(*types.Named)
	if named == nil {
		return
	}
	if got, want := doc.typeParams(spec.TypeParams), r.typeParams(named.TypeParams()); got != want {
		d.differs(pos, name, "type parameters "+got, "type parameters "+want)
		return
	}
	switch t := spec.Type.(type) {
	case *ast.StructType:
		d.structType(spec, t, named, doc, r)
	case *ast.InterfaceType:
		d.interfaceType(spec, t, named, doc, r)
	default:
		code := r.typ(named.Underlying())
		docType := doc.typ(spec.Type)
		// A listing may define a type by another named type, as the code does; compare what both stand for.
		if ref := d.resolve(spec.Type); ref != nil {
			docType = r.typ(ref.Underlying())
		}
		d.differs(pos, name, docType, code)
	}
}

func aliasWord(alias bool) string {
	if alias {
		return "an alias"
	}
	return "a defined type"
}

// resolve returns the type a listing names with an identifier or a qualified identifier, or nil.
func (d *declChecker) resolve(e ast.Expr) types.Type {
	var obj types.Object
	switch e := e.(type) {
	case *ast.Ident:
		obj = d.pkg.Scope().Lookup(e.Name)
		if obj == nil {
			obj = types.Universe.Lookup(e.Name)
		}
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		if !ok {
			return nil
		}
		for _, imp := range d.pkg.Imports() {
			if imp.Name() == x.Name {
				obj = imp.Scope().Lookup(e.Sel.Name)
			}
		}
	}
	if tn, ok := obj.(*types.TypeName); ok {
		return tn.Type()
	}
	return nil
}

func (d *declChecker) structType(spec *ast.TypeSpec, st *ast.StructType, named *types.Named, doc syntax, r renderer) {
	code, ok := named.Underlying().(*types.Struct)
	if !ok {
		d.differs(spec.Name.Pos(), spec.Name.Name, "a struct", r.typ(named.Underlying()))
		return
	}
	field := func(name string) *types.Var {
		for i := range code.NumFields() {
			if code.Field(i).Name() == name {
				return code.Field(i)
			}
		}
		return nil
	}
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 { // embedded
			name := embeddedName(f.Type)
			if got := field(name); got == nil || !got.Embedded() {
				d.report(f.Type.Pos(), spec.Name.Name+"."+name, "missing: %s embeds no %s", spec.Name.Name, name)
			}
			continue
		}
		for _, n := range f.Names {
			if !n.IsExported() {
				continue
			}
			full := spec.Name.Name + "." + n.Name
			got := field(n.Name)
			if got == nil {
				d.report(n.Pos(), full, "missing: %s has no field %s", spec.Name.Name, n.Name)
				continue
			}
			d.differs(n.Pos(), full, doc.typ(f.Type), r.typ(got.Type()))
		}
	}
}

func embeddedName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return embeddedName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.IndexExpr:
		return embeddedName(e.X)
	case *ast.IndexListExpr:
		return embeddedName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}

func (d *declChecker) interfaceType(spec *ast.TypeSpec, it *ast.InterfaceType, named *types.Named, doc syntax,
	r renderer) {
	pos, name := spec.Name.Pos(), spec.Name.Name
	code, ok := named.Underlying().(*types.Interface)
	if !ok {
		d.differs(pos, name, "an interface", r.typ(named.Underlying()))
		return
	}
	if !code.IsMethodSet() || hasTypeTerms(it) {
		d.differs(pos, name, doc.typ(it), normalise(types.TypeString(code, r.qualify)))
		return
	}
	listed := map[string]bool{}
	for _, f := range it.Methods.List {
		if len(f.Names) == 0 { // an embedded interface lists its methods too
			if t := d.resolve(f.Type); t != nil {
				if e, ok := t.Underlying().(*types.Interface); ok {
					for i := range e.NumMethods() {
						listed[e.Method(i).Name()] = true
					}
				}
			}
			continue
		}
		ft, ok := f.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, n := range f.Names {
			if !n.IsExported() {
				continue
			}
			listed[n.Name] = true
			full := name + "." + n.Name
			obj, _, _ := types.LookupFieldOrMethod(code, false, d.pkg, n.Name)
			m, ok := obj.(*types.Func)
			if !ok {
				d.report(n.Pos(), full, "missing: %s has no method %s", name, n.Name)
				continue
			}
			d.differs(n.Pos(), full, n.Name+doc.signature(ft, false),
				n.Name+r.signature(m.Type().(*types.Signature), false))
		}
	}
	for i := range code.NumMethods() {
		m := code.Method(i)
		if m.Exported() && !listed[m.Name()] {
			d.report(pos, name+"."+m.Name(), "incomplete: the listing of %s leaves out %s", name, m.Name())
		}
	}
}

func hasTypeTerms(it *ast.InterfaceType) bool {
	for _, f := range it.Methods.List {
		if len(f.Names) == 0 {
			if _, ok := f.Type.(*ast.Ident); !ok {
				if _, ok := f.Type.(*ast.SelectorExpr); !ok {
					return true
				}
			}
		}
	}
	return false
}

// valueSpec checks a var or const spec; index is its place in its group, the value of iota there.
func (d *declChecker) valueSpec(tok token.Token, spec *ast.ValueSpec, index int) {
	want := "var"
	if tok == token.CONST {
		want = "const"
	}
	r := renderer{home: d.pkg, rename: true}
	doc := newSyntax(nil, nil)
	for i, n := range spec.Names {
		if !n.IsExported() {
			continue
		}
		obj := d.lookup(n.Pos(), n.Name, want)
		if obj == nil {
			continue
		}
		if spec.Type != nil {
			d.differs(n.Pos(), n.Name, doc.typ(spec.Type), r.typ(obj.Type()))
		}
		c, ok := obj.(*types.Const)
		if !ok || i >= len(spec.Values) {
			continue
		}
		if spec.Type == nil {
			if b, ok := c.Type().(*types.Basic); !ok || b.Info()&types.IsUntyped == 0 {
				d.report(n.Pos(), n.Name, "differs: the doc has an untyped constant, the code a %s", r.typ(c.Type()))
				continue
			}
		}
		expr := iotaWord.ReplaceAllString(source(spec.Values[i]), strconv.Itoa(index))
		tv, err := types.Eval(token.NewFileSet(), d.pkg, token.NoPos, expr)
		if err != nil || tv.Value == nil {
			d.report(n.Pos(), n.Name, "differs: the doc's value %s doesn't evaluate in %s", source(spec.Values[i]), d.path)
			continue
		}
		if !constantsEqual(tv.Value, c.Val()) {
			d.differs(n.Pos(), n.Name, "the value "+tv.Value.ExactString(), "the value "+c.Val().ExactString())
		}
	}
}

// constantsEqual reports whether two constant values are equal, false when their kinds can't be compared.
func constantsEqual(a, b constant.Value) bool {
	numeric := func(v constant.Value) bool {
		k := v.Kind()
		return k == constant.Int || k == constant.Float || k == constant.Complex
	}
	if a.Kind() != b.Kind() && (!numeric(a) || !numeric(b)) {
		return false
	}
	return constant.Compare(a, token.EQL, b)
}

func source(n ast.Node) string {
	var b strings.Builder
	_ = printer.Fprint(&b, token.NewFileSet(), n)
	return b.String()
}

// syntax renders a listing's type expressions as renderer renders types, with its type parameters by position.
type syntax struct {
	params map[string]string
}

func newSyntax(tps *ast.FieldList, recvParams []*ast.Ident) syntax {
	s := syntax{params: map[string]string{}}
	i := 0
	if tps != nil {
		for _, f := range tps.List {
			for _, n := range f.Names {
				s.params[n.Name] = fmt.Sprintf("$%d", i)
				i++
			}
		}
	}
	for j, id := range recvParams {
		s.params[id.Name] = fmt.Sprintf("$%d", j)
	}
	return s
}

func (s syntax) typ(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		if p, ok := s.params[e.Name]; ok {
			return p
		}
		return e.Name
	case *ast.SelectorExpr:
		return s.typ(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + s.typ(e.X)
	case *ast.ParenExpr:
		return s.typ(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + s.typ(e.Elt)
		}
		return "[" + source(e.Len) + "]" + s.typ(e.Elt)
	case *ast.Ellipsis:
		return "..." + s.typ(e.Elt)
	case *ast.MapType:
		return "map[" + s.typ(e.Key) + "]" + s.typ(e.Value)
	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return "chan<- " + s.typ(e.Value)
		case ast.RECV:
			return "<-chan " + s.typ(e.Value)
		}
		return "chan " + s.typ(e.Value)
	case *ast.FuncType:
		return "func" + s.signature(e, false)
	case *ast.IndexExpr:
		return s.typ(e.X) + "[" + s.typ(e.Index) + "]"
	case *ast.IndexListExpr:
		var ix []string
		for _, i := range e.Indices {
			ix = append(ix, s.typ(i))
		}
		return s.typ(e.X) + "[" + strings.Join(ix, ", ") + "]"
	case *ast.InterfaceType:
		if len(e.Methods.List) == 0 {
			return "any"
		}
	case *ast.UnaryExpr:
		return e.Op.String() + s.typ(e.X)
	case *ast.BinaryExpr:
		return s.typ(e.X) + " " + e.Op.String() + " " + s.typ(e.Y)
	}
	return normalise(source(e))
}

func (s syntax) fields(fl *ast.FieldList) []string {
	if fl == nil {
		return nil
	}
	var out []string
	for _, f := range fl.List {
		for range max(1, len(f.Names)) {
			out = append(out, s.typ(f.Type))
		}
	}
	return out
}

func (s syntax) signature(ft *ast.FuncType, withTypeParams bool) string {
	var b strings.Builder
	if withTypeParams {
		b.WriteString(s.typeParams(ft.TypeParams))
	}
	b.WriteString("(" + strings.Join(s.fields(ft.Params), ", ") + ")")
	b.WriteString(resultList(s.fields(ft.Results)))
	return b.String()
}

func (s syntax) typeParams(tps *ast.FieldList) string {
	if tps == nil || len(tps.List) == 0 {
		return ""
	}
	var ps []string
	for _, f := range tps.List {
		for _, n := range f.Names {
			ps = append(ps, s.typ(n)+" "+s.typ(f.Type))
		}
	}
	return "[" + strings.Join(ps, ", ") + "]"
}

var (
	inlineCode = regexp.MustCompile("`([^`]+)`")
	identPath  = regexp.MustCompile(`^[A-Za-z_]\w*(\.[A-Za-z_]\w*)*$`)
	allCaps    = regexp.MustCompile(`^[A-Z0-9_]+$`)
)

// Names checks the backticked Go identifiers in doc, the markdown file called name, against pkgs (DL-267): each must
// name a package-level name, method or field of one of them, qualified by its package's name or not. All-capitals
// names, names qualified by a package outside pkgs, and names in allow are skipped, as is everything in code blocks.
func Names(pkgs map[string]*types.Package, name string, doc []byte, allow []string) []Finding {
	byName := map[string][]*types.Package{}
	for _, p := range pkgs {
		byName[p.Name()] = append(byName[p.Name()], p)
	}
	var findings []Finding
	inBlock := false
	for i, line := range strings.Split(string(doc), "\n") {
		if fenceOpen.MatchString(line) || fenceClose.MatchString(line) {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			continue
		}
		for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
			ident := m[1]
			if j := strings.IndexAny(ident, "(["); j >= 0 { // a call's arguments, or type arguments
				ident = ident[:j]
			}
			segs := strings.Split(ident, ".")
			last := segs[len(segs)-1]
			switch {
			case !identPath.MatchString(ident), last == "" || last[0] < 'A' || last[0] > 'Z', allCaps.MatchString(ident),
				slices.Contains(allow, ident):
				continue
			}
			if !knownName(pkgs, byName, segs) {
				findings = append(findings, Finding{Doc: name, Line: i + 1, Name: ident,
					Message: "missing: no released package has " + ident})
			}
		}
	}
	return findings
}

// knownName reports whether segs name something in pkgs, or are qualified by a package outside them.
func knownName(pkgs map[string]*types.Package, byName map[string][]*types.Package, segs []string) bool {
	if ours, ok := byName[segs[0]]; ok && len(segs) > 1 {
		for _, p := range ours {
			if memberOf(p, segs[1:]) {
				return true
			}
		}
		return false
	}
	if c := segs[0][0]; c >= 'a' && c <= 'z' {
		return true // qualified by a package outside this repository, such as slog
	}
	for _, p := range pkgs {
		if memberOf(p, segs) {
			return true
		}
	}
	if len(segs) == 1 { // a bare method or field name
		for _, p := range pkgs {
			for _, n := range p.Scope().Names() {
				if tn, ok := p.Scope().Lookup(n).(*types.TypeName); ok && memberOf(p, []string{n, segs[0]}) && tn.Exported() {
					return true
				}
			}
		}
	}
	return false
}

// memberOf reports whether segs, a name or a type's name and one of its fields or methods, is in p.
func memberOf(p *types.Package, segs []string) bool {
	obj := p.Scope().Lookup(segs[0])
	if obj == nil || !obj.Exported() {
		return false
	}
	switch len(segs) {
	case 1:
		return true
	case 2:
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return false
		}
		m, _, _ := types.LookupFieldOrMethod(tn.Type(), true, p, segs[1])
		return m != nil && m.Exported()
	}
	return false
}
