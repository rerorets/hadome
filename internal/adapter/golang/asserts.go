package golang

import (
	"go/ast"
	"go/types"
)

// alwaysTrue recognises testify assertions that cannot fail: True(t, true) and Equal(t, x, x) where
// x calls nothing.
func alwaysTrue(name string, c *ast.CallExpr) bool {
	switch name {
	case "True":
		if len(c.Args) >= 2 {
			id, ok := c.Args[1].(*ast.Ident)
			return ok && id.Name == "true"
		}
	case "False":
		if len(c.Args) >= 2 {
			id, ok := c.Args[1].(*ast.Ident)
			return ok && id.Name == "false"
		}
	case "Equal", "Exactly", "EqualValues":
		return len(c.Args) >= 3 && types.ExprString(c.Args[1]) == types.ExprString(c.Args[2]) && !holdsCall(c.Args[1])
	}
	return false
}

// holdsCall reports whether an expression calls something. Two calls spelled the same may return
// different values, so comparing them is a check that can fail, not a value compared with itself.
func holdsCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}
