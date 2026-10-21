package apicheck

import (
	"go/types"
	"slices"
	"strings"
)

// API renders pkg's exported API, one declaration per line, sorted (DL-257).
func API(pkg *types.Package) []string {
	r := renderer{home: pkg}
	var lines []string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch obj := obj.(type) {
		case *types.Const:
			lines = append(lines, renderConst(r, obj))
		case *types.Var:
			lines = append(lines, "var "+name+" "+r.typ(obj.Type()))
		case *types.Func:
			lines = append(lines, "func "+name+r.signature(obj.Type().(*types.Signature), true))
		case *types.TypeName:
			lines = append(lines, renderType(r, obj)...)
		}
	}
	slices.Sort(lines)
	return lines
}

func renderConst(r renderer, c *types.Const) string {
	val := c.Val().ExactString()
	if b, ok := c.Type().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return "const " + c.Name() + " = " + val
	}
	return "const " + c.Name() + " " + r.typ(c.Type()) + " = " + val
}

func renderType(r renderer, tn *types.TypeName) []string {
	name := tn.Name()
	if tn.IsAlias() {
		return []string{"type " + name + " = " + r.typ(types.Unalias(tn.Type()))}
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return []string{"type " + name + " " + r.typ(tn.Type().Underlying())}
	}
	head := "type " + name + r.typeParams(named.TypeParams())
	var lines []string
	switch u := named.Underlying().(type) {
	case *types.Struct:
		lines = append(lines, head+" struct")
		for i := range u.NumFields() {
			f := u.Field(i)
			switch {
			case f.Embedded():
				lines = append(lines, head+" struct, embedded "+r.typ(f.Type()))
			case f.Exported():
				lines = append(lines, head+" struct, "+f.Name()+" "+r.typ(f.Type()))
			}
		}
	case *types.Interface:
		if !u.IsMethodSet() {
			return []string{head + " " + normalise(types.TypeString(u, r.qualify))}
		}
		lines = append(lines, head+" interface")
		sealed := false
		for i := range u.NumMethods() {
			m := u.Method(i)
			if !m.Exported() {
				sealed = true
				continue
			}
			lines = append(lines, head+" interface, "+m.Name()+r.signature(m.Type().(*types.Signature), false))
		}
		if sealed {
			lines = append(lines, head+" interface, unexported methods")
		}
		return lines
	default:
		lines = append(lines, head+" "+r.typ(u))
	}
	return append(lines, renderMethods(r, named)...)
}

// renderMethods lists the exported methods of named's pointer type, promoted ones included: with a value receiver
// when a value has the method, else a pointer one.
func renderMethods(r renderer, named *types.Named) []string {
	recv := named.Obj().Name()
	if tps := named.TypeParams(); tps != nil && tps.Len() > 0 {
		var ps []string
		for i := range tps.Len() {
			ps = append(ps, tps.At(i).Obj().Name())
		}
		recv += "[" + strings.Join(ps, ", ") + "]"
	}
	values := types.NewMethodSet(named)
	pointers := types.NewMethodSet(types.NewPointer(named))
	var lines []string
	for i := range pointers.Len() {
		m := pointers.At(i).Obj()
		if !m.Exported() {
			continue
		}
		rc := "(*" + recv + ")"
		if values.Lookup(m.Pkg(), m.Name()) != nil {
			rc = "(" + recv + ")"
		}
		lines = append(lines, "method "+rc+" "+m.Name()+r.signature(m.Type().(*types.Signature), false))
	}
	return lines
}
