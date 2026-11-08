package golang

import (
	"go/ast"
	"go/constant"
	"go/token"
)

// zeroConstant reports whether a constant's value is the zero value: 0, "" or false.
func zeroConstant(v constant.Value) bool {
	switch v.Kind() {
	case constant.String:
		return constant.StringVal(v) == ""
	case constant.Bool:
		return !constant.BoolVal(v)
	case constant.Int, constant.Float, constant.Complex:
		return constant.Sign(v) == 0
	}
	return false
}

// constValue evaluates a constant expression of literals, false and true, iota (at the given
// index), the constants known so far, conversions, and the unary and binary operators on numbers,
// strings and booleans. Anything else, and operands an operator does not take (which do not
// compile), give an unknown value.
func constValue(e ast.Expr, iota int, known map[string]constant.Value) constant.Value {
	unknown := constant.MakeUnknown()
	switch e := e.(type) {
	case *ast.ParenExpr:
		return constValue(e.X, iota, known)
	case *ast.BasicLit:
		return constant.MakeFromLiteral(e.Value, e.Kind, 0)
	case *ast.Ident:
		return identValue(e.Name, iota, known)
	case *ast.CallExpr:
		if conversion(e) {
			return constValue(e.Args[0], iota, known)
		}
	case *ast.UnaryExpr:
		x := constValue(e.X, iota, known)
		if (e.Op == token.ADD || e.Op == token.SUB) && numeric(x) || e.Op == token.NOT && x.Kind() == constant.Bool {
			return constant.UnaryOp(e.Op, x, 0)
		}
	case *ast.BinaryExpr:
		return binaryValue(e.Op, constValue(e.X, iota, known), constValue(e.Y, iota, known))
	}
	return unknown
}

// identValue is the value of a name in a constant expression: false, true, iota, or a constant known
// so far.
func identValue(name string, iota int, known map[string]constant.Value) constant.Value {
	switch name {
	case "false", "true":
		return constant.MakeBool(name == "true")
	case "iota":
		return constant.MakeInt64(int64(iota))
	}
	if v, ok := known[name]; ok {
		return v
	}
	return constant.MakeUnknown()
}

// binaryValue applies a binary operator to two constant values, or gives an unknown value when the
// operator does not take them. Shifts, comparisons and bitwise operators are not read.
func binaryValue(op token.Token, x, y constant.Value) constant.Value {
	if op == token.QUO || op == token.REM {
		if !numeric(y) || constant.Sign(y) == 0 {
			return constant.MakeUnknown()
		}
		if x.Kind() == constant.Int && y.Kind() == constant.Int {
			if op == token.QUO {
				op = token.QUO_ASSIGN // integer division, as Go divides two integer constants
			}
			return constant.BinaryOp(x, op, y)
		}
	}
	if !operandsFit(op, x, y) {
		return constant.MakeUnknown()
	}
	return constant.BinaryOp(x, op, y)
}

// operandsFit reports whether an operator other than integer division and remainder takes the two
// values.
func operandsFit(op token.Token, x, y constant.Value) bool {
	switch op {
	case token.ADD:
		return numeric(x) && numeric(y) || x.Kind() == constant.String && y.Kind() == constant.String
	case token.SUB, token.MUL, token.QUO:
		return numeric(x) && numeric(y)
	case token.LAND, token.LOR:
		return x.Kind() == constant.Bool && y.Kind() == constant.Bool
	}
	return false
}

// numeric reports whether a value is a number: an integer, a rune, a float or a complex number.
func numeric(v constant.Value) bool { return constant.ToComplex(v).Kind() == constant.Complex }

// conversion reports a call that may be a conversion such as Code(0) or pkg.Code(0), whose value is
// its argument's. A builtin such as len or real gives zero for a zero argument too, so taking the
// argument's value keeps whether it is zero; unsafe.Sizeof and the like do not.
func conversion(c *ast.CallExpr) bool {
	if len(c.Args) != 1 {
		return false
	}
	if s, ok := c.Fun.(*ast.SelectorExpr); ok {
		if x, ok := s.X.(*ast.Ident); ok && x.Name == "unsafe" {
			return false
		}
	}
	return true
}
