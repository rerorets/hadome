package golang

import (
	"go/ast"
	"go/token"
)

// errorsAs marks the variable errors.As fills, &v, as taken out by asking whether the error holds
// its type.
func (w *walker) errorsAs(c *ast.CallExpr, p, name string) {
	if p != "errors" && p != "golang.org/x/xerrors" || name != "As" || len(c.Args) != 2 {
		return
	}
	u, ok := ast.Unparen(c.Args[1]).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return
	}
	if v, ok := ast.Unparen(u.X).(*ast.Ident); ok {
		if t, ok := w.lookup(v.Name); ok && t != nil {
			w.assign(v.Name, t.askedFor())
		}
	}
}

// typeSwitch walks a type switch. The variable it binds takes, in a case of one type, that type,
// taken out by asking; in any other case, the type of the value switched on.
func (w *walker) typeSwitch(n *ast.TypeSwitchStmt) {
	w.inspect(n.Init)
	w.inspect(n.Assign)
	var name string
	var switched *vtype
	if as, ok := n.Assign.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
		id, _ := as.Lhs[0].(*ast.Ident)
		x, _ := as.Rhs[0].(*ast.TypeAssertExpr)
		if id != nil && x != nil && id.Name != "_" {
			name, switched = id.Name, w.valueType(x.X)
		}
	}
	w.branches++
	for _, s := range n.Body.List {
		cc := s.(*ast.CaseClause)
		if name != "" {
			t := switched
			if len(cc.List) == 1 && !isNil(cc.List[0]) {
				t = w.resolve(cc.List[0]).askedFor()
			}
			w.local.vars[name] = t
		}
		for _, st := range cc.Body {
			w.inspect(st)
		}
	}
	w.branches--
}

func isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}
