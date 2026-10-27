package apicheck

import (
	"fmt"
	"go/types"
	"strings"
)

// renderer turns types into the text that both the API files and the listing comparisons use: parameter names left
// out, other packages' types qualified by their package's name, an empty interface written any. When rename is set,
// type parameters are written by position ($0, $1, …), so a listing can name them differently from the code.
type renderer struct {
	home   *types.Package
	rename bool
}

func (r renderer) qualify(p *types.Package) string {
	if p == nil || p == r.home {
		return ""
	}
	return p.Name()
}

func (r renderer) typ(t types.Type) string {
	switch t := t.(type) {
	case *types.Alias:
		return r.named(t.Obj(), t.TypeArgs())
	case *types.Named:
		return r.named(t.Obj(), t.TypeArgs())
	case *types.TypeParam:
		if r.rename {
			return fmt.Sprintf("$%d", t.Index())
		}
		return t.Obj().Name()
	case *types.Pointer:
		return "*" + r.typ(t.Elem())
	case *types.Slice:
		return "[]" + r.typ(t.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), r.typ(t.Elem()))
	case *types.Map:
		return "map[" + r.typ(t.Key()) + "]" + r.typ(t.Elem())
	case *types.Chan:
		switch t.Dir() {
		case types.SendOnly:
			return "chan<- " + r.typ(t.Elem())
		case types.RecvOnly:
			return "<-chan " + r.typ(t.Elem())
		}
		return "chan " + r.typ(t.Elem())
	case *types.Signature:
		return "func" + r.signature(t, false)
	case *types.Interface:
		if t.Empty() {
			return "any"
		}
	}
	return normalise(types.TypeString(t, r.qualify))
}

func (r renderer) named(obj *types.TypeName, args *types.TypeList) string {
	name := obj.Name()
	if q := r.qualify(obj.Pkg()); q != "" {
		name = q + "." + name
	}
	if args == nil || args.Len() == 0 {
		return name
	}
	var as []string
	for i := range args.Len() {
		as = append(as, r.typ(args.At(i)))
	}
	return name + "[" + strings.Join(as, ", ") + "]"
}

// signature renders sig's parameter and result types, with its type parameters first when withTypeParams is set.
func (r renderer) signature(sig *types.Signature, withTypeParams bool) string {
	var b strings.Builder
	if withTypeParams {
		b.WriteString(r.typeParams(sig.TypeParams()))
	}
	var params []string
	for i := range sig.Params().Len() {
		t := sig.Params().At(i).Type()
		if sig.Variadic() && i == sig.Params().Len()-1 {
			params = append(params, "..."+r.typ(t.(*types.Slice).Elem()))
		} else {
			params = append(params, r.typ(t))
		}
	}
	b.WriteString("(" + strings.Join(params, ", ") + ")")
	var results []string
	for i := range sig.Results().Len() {
		results = append(results, r.typ(sig.Results().At(i).Type()))
	}
	b.WriteString(resultList(results))
	return b.String()
}

func (r renderer) typeParams(tps *types.TypeParamList) string {
	if tps == nil || tps.Len() == 0 {
		return ""
	}
	var ps []string
	for i := range tps.Len() {
		ps = append(ps, r.typ(tps.At(i))+" "+r.typ(tps.At(i).Constraint()))
	}
	return "[" + strings.Join(ps, ", ") + "]"
}

// resultList writes results as Go does: nothing, one type, or a parenthesised list.
func resultList(results []string) string {
	switch len(results) {
	case 0:
		return ""
	case 1:
		return " " + results[0]
	}
	return " (" + strings.Join(results, ", ") + ")"
}

// normalise collapses runs of white space, and drops the space just inside braces, so go/printer's and go/types'
// renderings of the same type agree.
func normalise(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "{ ", "{")
	return strings.ReplaceAll(s, " }", "}")
}
