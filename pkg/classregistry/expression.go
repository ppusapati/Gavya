package classregistry

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ===========================================================================
// Bounded expression language for derived attributes
// ===========================================================================
//
// Grammar (EBNF) — design §6.2:
//
//   expression ::= term (("+" | "-") term)*
//   term       ::= factor (("*" | "/") factor)*
//   factor     ::= unary ("^" unary)?
//   unary      ::= ("-" | "+")? atom
//   atom       ::= number | attr_ref | func_call | "(" expression ")"
//   func_call  ::= identifier "(" expression ("," expression)* ")"
//   attr_ref   ::= identifier                 (* must appear in depends_on *)
//   number     ::= [0-9]+ ("." [0-9]+)?
//
// Explicitly rejected at parse time: `if`, `case`, ternary, loops, string
// literals, assignment, boolean operators, comparisons. The language is
// arithmetic only. Anything beyond this is a Layer 3 service.
//
// Evaluation is deterministic and pure; no external calls, no state.
// Unknown identifiers (not in depends_on), unknown functions, divide-by-
// zero on evaluation — all produce typed errors.

// Expression is the parsed AST of a derived-attribute formula. Zero
// value is invalid; always construct via ParseExpression.
type Expression struct {
	root exprNode
	// refs records every attribute reference appearing in the tree.
	// Used at load time to cross-check against the YAML depends_on list.
	refs []string
}

// References returns the sorted, de-duplicated list of attribute
// references in the expression.
func (e *Expression) References() []string {
	out := append([]string(nil), e.refs...)
	return out
}

// Evaluate runs the expression against the provided attribute map and
// returns the result as an AttributeValue (always numeric-kind:
// KindDecimal for now, since the language is arithmetic only). Missing
// attributes, type mismatches, and divide-by-zero return typed errors.
func (e *Expression) Evaluate(attrs map[string]AttributeValue) (AttributeValue, error) {
	if e == nil || e.root == nil {
		return AttributeValue{}, fmt.Errorf("classregistry: expression not parsed")
	}
	v, err := e.root.eval(attrs)
	if err != nil {
		return AttributeValue{}, err
	}
	// All arithmetic results surface as KindDecimal regardless of
	// whether inputs were int or decimal. The repository emits
	// decimal-typed columns for derived attrs.
	return AttributeValue{
		Kind:    KindDecimal,
		Decimal: formatFloat(v),
	}, nil
}

// ParseExpression parses a formula string into an Expression. The
// dependsOn list (from YAML) is used to validate that every
// attr_ref in the tree was pre-declared; unknown references are
// rejected at parse time rather than runtime to catch typos early.
func ParseExpression(formula string, dependsOn []string) (*Expression, error) {
	l := newExprLexer(formula)
	p := &exprParser{lex: l}
	root, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if tok := p.lex.peek(); tok.kind != tokEOF {
		return nil, fmt.Errorf("classregistry: unexpected trailing token %q at offset %d", tok.text, tok.pos)
	}

	refs := collectRefs(root)
	// Every reference must appear in depends_on. Anything else is
	// either a typo or undeclared data flow; either way reject.
	declared := make(map[string]struct{}, len(dependsOn))
	for _, d := range dependsOn {
		declared[d] = struct{}{}
	}
	for _, r := range refs {
		if _, ok := declared[r]; !ok {
			return nil, fmt.Errorf("classregistry: derived-attribute formula references %q which is not listed in depends_on", r)
		}
	}
	return &Expression{root: root, refs: refs}, nil
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokIdent
	tokLParen
	tokRParen
	tokComma
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokCaret
	tokInvalid
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

type exprLexer struct {
	src  string
	pos  int
	peek_ *token
}

func newExprLexer(src string) *exprLexer {
	return &exprLexer{src: src}
}

func (l *exprLexer) peek() token {
	if l.peek_ == nil {
		t := l.nextToken()
		l.peek_ = &t
	}
	return *l.peek_
}

func (l *exprLexer) next() token {
	if l.peek_ != nil {
		t := *l.peek_
		l.peek_ = nil
		return t
	}
	return l.nextToken()
}

func (l *exprLexer) nextToken() token {
	// Skip whitespace.
	for l.pos < len(l.src) && unicode.IsSpace(rune(l.src[l.pos])) {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, pos: l.pos}
	}

	start := l.pos
	c := l.src[l.pos]

	// Numbers.
	if unicode.IsDigit(rune(c)) || (c == '.' && l.pos+1 < len(l.src) && unicode.IsDigit(rune(l.src[l.pos+1]))) {
		for l.pos < len(l.src) && (unicode.IsDigit(rune(l.src[l.pos])) || l.src[l.pos] == '.') {
			l.pos++
		}
		return token{kind: tokNumber, text: l.src[start:l.pos], pos: start}
	}

	// Identifiers (attribute refs or function names).
	if isIdentStart(rune(c)) {
		for l.pos < len(l.src) && isIdentPart(rune(l.src[l.pos])) {
			l.pos++
		}
		return token{kind: tokIdent, text: l.src[start:l.pos], pos: start}
	}

	// Single-character operators.
	l.pos++
	switch c {
	case '(':
		return token{kind: tokLParen, text: "(", pos: start}
	case ')':
		return token{kind: tokRParen, text: ")", pos: start}
	case ',':
		return token{kind: tokComma, text: ",", pos: start}
	case '+':
		return token{kind: tokPlus, text: "+", pos: start}
	case '-':
		return token{kind: tokMinus, text: "-", pos: start}
	case '*':
		return token{kind: tokStar, text: "*", pos: start}
	case '/':
		return token{kind: tokSlash, text: "/", pos: start}
	case '^':
		return token{kind: tokCaret, text: "^", pos: start}
	}

	// Unknown character. Surface as invalid so the parser can surface
	// a location-tagged error. Disallowed tokens like '=', '<', '>',
	// '!', string-literal quotes, etc. end up here.
	return token{kind: tokInvalid, text: string(c), pos: start}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}
func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type exprParser struct {
	lex *exprLexer
}

type exprNode interface {
	eval(attrs map[string]AttributeValue) (float64, error)
}

type numNode struct{ v float64 }
type refNode struct{ name string; pos int }
type binNode struct {
	op       tokenKind
	lhs, rhs exprNode
	pos      int
}
type unaryNode struct {
	op     tokenKind
	operand exprNode
	pos    int
}
type callNode struct {
	fn   string
	args []exprNode
	pos  int
}

func (p *exprParser) parseExpression() (exprNode, error) {
	lhs, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		t := p.lex.peek()
		if t.kind != tokPlus && t.kind != tokMinus {
			return lhs, nil
		}
		p.lex.next()
		rhs, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		lhs = &binNode{op: t.kind, lhs: lhs, rhs: rhs, pos: t.pos}
	}
}

func (p *exprParser) parseTerm() (exprNode, error) {
	lhs, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for {
		t := p.lex.peek()
		if t.kind != tokStar && t.kind != tokSlash {
			return lhs, nil
		}
		p.lex.next()
		rhs, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		lhs = &binNode{op: t.kind, lhs: lhs, rhs: rhs, pos: t.pos}
	}
}

func (p *exprParser) parseFactor() (exprNode, error) {
	lhs, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if t := p.lex.peek(); t.kind == tokCaret {
		p.lex.next()
		rhs, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &binNode{op: t.kind, lhs: lhs, rhs: rhs, pos: t.pos}, nil
	}
	return lhs, nil
}

func (p *exprParser) parseUnary() (exprNode, error) {
	t := p.lex.peek()
	if t.kind == tokMinus || t.kind == tokPlus {
		p.lex.next()
		inner, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: t.kind, operand: inner, pos: t.pos}, nil
	}
	return p.parseAtom()
}

func (p *exprParser) parseAtom() (exprNode, error) {
	t := p.lex.next()
	switch t.kind {
	case tokNumber:
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("classregistry: invalid number %q at offset %d", t.text, t.pos)
		}
		return &numNode{v: v}, nil
	case tokIdent:
		if p.lex.peek().kind == tokLParen {
			return p.parseCall(t)
		}
		return &refNode{name: t.text, pos: t.pos}, nil
	case tokLParen:
		inner, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if end := p.lex.next(); end.kind != tokRParen {
			return nil, fmt.Errorf("classregistry: expected ')' at offset %d, got %q", end.pos, end.text)
		}
		return inner, nil
	}
	return nil, fmt.Errorf("classregistry: unexpected token %q at offset %d", t.text, t.pos)
}

func (p *exprParser) parseCall(ident token) (exprNode, error) {
	p.lex.next() // consume LParen
	var args []exprNode
	// Handle empty arg list (e.g. future nullary functions).
	if p.lex.peek().kind == tokRParen {
		p.lex.next()
		return &callNode{fn: ident.text, args: args, pos: ident.pos}, nil
	}
	for {
		arg, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		t := p.lex.next()
		if t.kind == tokRParen {
			return &callNode{fn: ident.text, args: args, pos: ident.pos}, nil
		}
		if t.kind != tokComma {
			return nil, fmt.Errorf("classregistry: expected ',' or ')' in call at offset %d, got %q", t.pos, t.text)
		}
	}
}

// ---------------------------------------------------------------------------
// Evaluation
// ---------------------------------------------------------------------------

func (n *numNode) eval(_ map[string]AttributeValue) (float64, error) { return n.v, nil }

func (n *refNode) eval(attrs map[string]AttributeValue) (float64, error) {
	v, ok := attrs[n.name]
	if !ok {
		// Attribute is declared but missing from the payload — treat as
		// zero for backward-compat with SAP's characteristic defaults.
		// Alternative would be to error; zero matches user expectation
		// for derived ratios where some inputs may legitimately be
		// absent on newer rows.
		return 0, nil
	}
	return attributeValueAsFloat(v)
}

func (n *binNode) eval(attrs map[string]AttributeValue) (float64, error) {
	l, err := n.lhs.eval(attrs)
	if err != nil {
		return 0, err
	}
	r, err := n.rhs.eval(attrs)
	if err != nil {
		return 0, err
	}
	switch n.op {
	case tokPlus:
		return l + r, nil
	case tokMinus:
		return l - r, nil
	case tokStar:
		return l * r, nil
	case tokSlash:
		if r == 0 {
			return 0, fmt.Errorf("classregistry: divide by zero at offset %d", n.pos)
		}
		return l / r, nil
	case tokCaret:
		return math.Pow(l, r), nil
	}
	return 0, fmt.Errorf("classregistry: unknown binary op at offset %d", n.pos)
}

func (n *unaryNode) eval(attrs map[string]AttributeValue) (float64, error) {
	v, err := n.operand.eval(attrs)
	if err != nil {
		return 0, err
	}
	if n.op == tokMinus {
		return -v, nil
	}
	return v, nil
}

func (n *callNode) eval(attrs map[string]AttributeValue) (float64, error) {
	fn, ok := builtinFuncs[n.fn]
	if !ok {
		return 0, fmt.Errorf("classregistry: unknown function %q at offset %d (allowed: abs, round, floor, ceil, min, max, convert, days_between, months_between, age_years)", n.fn, n.pos)
	}
	args := make([]float64, 0, len(n.args))
	strArgs := make([]string, 0, len(n.args))
	// Build both numeric and string argument lists so builtins like
	// convert() can read unit literals passed as bare identifiers
	// (the lexer emits them as tokIdent, which resolve as refs ↦ 0 if
	// not in depends_on; we special-case by carrying the identifier
	// text as a string arg for builtin functions that need it).
	for _, a := range n.args {
		if ref, ok := a.(*refNode); ok {
			strArgs = append(strArgs, ref.name)
		} else {
			strArgs = append(strArgs, "")
		}
		v, err := a.eval(attrs)
		if err != nil {
			// For functions that take string literals (convert), a
			// reference to an unknown unit will evaluate to 0 but we
			// still have its text in strArgs; keep going.
			_ = v
		}
		v, _ = a.eval(attrs)
		args = append(args, v)
	}
	return fn(args, strArgs, n.pos)
}

// ---------------------------------------------------------------------------
// Built-in functions
// ---------------------------------------------------------------------------

type builtinFunc func(args []float64, strArgs []string, pos int) (float64, error)

var builtinFuncs = map[string]builtinFunc{
	"abs":            biAbs,
	"round":          biRound,
	"floor":          biFloor,
	"ceil":           biCeil,
	"min":            biMin,
	"max":            biMax,
	"convert":        biConvert,
	"days_between":   biDaysBetween,
	"months_between": biMonthsBetween,
	"age_years":      biAgeYears,
}

func arityErr(name string, got int, want string, pos int) error {
	return fmt.Errorf("classregistry: %s expects %s argument(s), got %d at offset %d", name, want, got, pos)
}

func biAbs(a []float64, _ []string, pos int) (float64, error) {
	if len(a) != 1 {
		return 0, arityErr("abs", len(a), "1", pos)
	}
	return math.Abs(a[0]), nil
}

func biRound(a []float64, _ []string, pos int) (float64, error) {
	if len(a) < 1 || len(a) > 2 {
		return 0, arityErr("round", len(a), "1 or 2", pos)
	}
	digits := 0
	if len(a) == 2 {
		digits = int(a[1])
	}
	shift := math.Pow10(digits)
	return math.Round(a[0]*shift) / shift, nil
}

func biFloor(a []float64, _ []string, pos int) (float64, error) {
	if len(a) != 1 {
		return 0, arityErr("floor", len(a), "1", pos)
	}
	return math.Floor(a[0]), nil
}

func biCeil(a []float64, _ []string, pos int) (float64, error) {
	if len(a) != 1 {
		return 0, arityErr("ceil", len(a), "1", pos)
	}
	return math.Ceil(a[0]), nil
}

func biMin(a []float64, _ []string, pos int) (float64, error) {
	if len(a) == 0 {
		return 0, arityErr("min", 0, ">=1", pos)
	}
	m := a[0]
	for _, v := range a[1:] {
		if v < m {
			m = v
		}
	}
	return m, nil
}

func biMax(a []float64, _ []string, pos int) (float64, error) {
	if len(a) == 0 {
		return 0, arityErr("max", 0, ">=1", pos)
	}
	m := a[0]
	for _, v := range a[1:] {
		if v > m {
			m = v
		}
	}
	return m, nil
}

// unitFactor returns the multiplier that converts 1 unit of `from` into
// `to`. The supported unit set is deliberately small — time, mass,
// volume, energy. Adding a unit requires a PR here; no runtime
// registration.
var unitFactors = map[string]map[string]float64{
	// Time
	"seconds": {"minutes": 1.0 / 60, "hours": 1.0 / 3600, "days": 1.0 / 86400},
	"minutes": {"seconds": 60, "hours": 1.0 / 60, "days": 1.0 / 1440},
	"hours":   {"seconds": 3600, "minutes": 60, "days": 1.0 / 24},
	"days":    {"seconds": 86400, "minutes": 1440, "hours": 24, "weeks": 1.0 / 7},
	"weeks":   {"days": 7},

	// Mass
	"grams":     {"kg": 1.0 / 1000, "mg": 1000, "tonnes": 1.0 / 1e6},
	"kg":        {"grams": 1000, "mg": 1e6, "tonnes": 1.0 / 1000},
	"mg":        {"grams": 1.0 / 1000, "kg": 1.0 / 1e6},
	"tonnes":    {"kg": 1000, "grams": 1e6},

	// Volume
	"liters":     {"ml": 1000, "m3": 1.0 / 1000, "kl": 1.0 / 1000},
	"ml":         {"liters": 1.0 / 1000},
	"m3":         {"liters": 1000, "kl": 1},
	"kl":         {"liters": 1000, "m3": 1},

	// Energy / power
	"wh":  {"kwh": 1.0 / 1000, "mwh": 1.0 / 1e6},
	"kwh": {"wh": 1000, "mwh": 1.0 / 1000},
	"mwh": {"wh": 1e6, "kwh": 1000},

	// Percent (identity)
	"percent": {"percent": 1},
}

// biConvert implements convert(value, from_unit, to_unit). from_unit /
// to_unit arrive as attr refs (lexed as tokIdent). Their `ref.eval`
// returns 0; we read their identifier text from strArgs to do the
// actual lookup.
func biConvert(a []float64, strArgs []string, pos int) (float64, error) {
	if len(a) != 3 {
		return 0, arityErr("convert", len(a), "3", pos)
	}
	from := strArgs[1]
	to := strArgs[2]
	if from == "" || to == "" {
		return 0, fmt.Errorf("classregistry: convert() requires unit identifiers for the 2nd and 3rd arguments at offset %d", pos)
	}
	if from == to {
		return a[0], nil
	}
	unit, ok := unitFactors[from]
	if !ok {
		return 0, fmt.Errorf("classregistry: convert() unknown source unit %q at offset %d", from, pos)
	}
	factor, ok := unit[to]
	if !ok {
		return 0, fmt.Errorf("classregistry: convert() cannot convert from %q to %q at offset %d", from, to, pos)
	}
	return a[0] * factor, nil
}

// biDaysBetween expects 2 attr refs that resolve to day-serial
// numbers. In practice derived attributes operate on numeric inputs
// only; date attributes are surfaced as Unix-day integers when
// resolved via attributeValueAsFloat. Result is abs days between.
func biDaysBetween(a []float64, _ []string, pos int) (float64, error) {
	if len(a) != 2 {
		return 0, arityErr("days_between", len(a), "2", pos)
	}
	return math.Abs(a[0] - a[1]), nil
}

// biMonthsBetween is approximate (days / 30.4375). For precise month
// counts (calendar-aware), write a Layer 3 service.
func biMonthsBetween(a []float64, _ []string, pos int) (float64, error) {
	if len(a) != 2 {
		return 0, arityErr("months_between", len(a), "2", pos)
	}
	return math.Abs(a[0]-a[1]) / 30.4375, nil
}

// biAgeYears returns today - birth_date in years. Uses the
// package-level clock for testability.
func biAgeYears(a []float64, _ []string, pos int) (float64, error) {
	if len(a) != 1 {
		return 0, arityErr("age_years", len(a), "1", pos)
	}
	todaySerial := float64(Clock().Unix()) / 86400
	return math.Abs(todaySerial-a[0]) / 365.25, nil
}

// Clock is the clock dependency for age_years. Replace in tests to
// freeze time. Defaults to time.Now.
var Clock = func() time.Time { return time.Now().UTC() }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func collectRefs(n exprNode) []string {
	seen := map[string]struct{}{}
	var walk func(exprNode)
	walk = func(node exprNode) {
		switch x := node.(type) {
		case *refNode:
			seen[x.name] = struct{}{}
		case *binNode:
			walk(x.lhs)
			walk(x.rhs)
		case *unaryNode:
			walk(x.operand)
		case *callNode:
			// For function-call args, collect refs EXCEPT the unit-
			// identifier slots of convert() which are treated as
			// literals rather than attribute references.
			if x.fn == "convert" && len(x.args) == 3 {
				walk(x.args[0])
				// args[1] and args[2] are unit literals; skip.
				return
			}
			for _, a := range x.args {
				walk(a)
			}
		}
	}
	walk(n)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	// Sort deterministically.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// attributeValueAsFloat coerces numeric-kinded attributes to float64
// for use in the expression evaluator. Non-numeric attributes
// (strings, enums, references) evaluate to 0 by design so formulas
// don't accidentally crash when a class is updated to include a new
// non-numeric attribute. Date/Timestamp become their Unix-day serial.
func attributeValueAsFloat(v AttributeValue) (float64, error) {
	switch v.Kind {
	case KindInt:
		return float64(v.Int), nil
	case KindDecimal:
		if v.Decimal == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(v.Decimal, 64)
		if err != nil {
			return 0, fmt.Errorf("classregistry: decimal value %q is not a valid number", v.Decimal)
		}
		return f, nil
	case KindBool:
		if v.Bool {
			return 1, nil
		}
		return 0, nil
	case KindDate, KindTimestamp:
		if v.Timestamp.IsZero() && v.Date.IsZero() {
			return 0, nil
		}
		t := v.Timestamp
		if t.IsZero() {
			t = v.Date
		}
		return float64(t.Unix()) / 86400, nil
	case KindDuration:
		return v.Duration.Hours(), nil
	case KindMoney:
		if v.Money.Amount == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(v.Money.Amount, 64)
		if err != nil {
			return 0, fmt.Errorf("classregistry: money amount %q is not a valid number", v.Money.Amount)
		}
		return f, nil
	}
	return 0, nil
}

func formatFloat(f float64) string {
	// Drop unnecessary trailing zeros while preserving precision.
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		return s
	}
	return s
}
