//go:build solver

package cloudflare

// JS deobfuscation pipeline for Cloudflare JSD scripts.
// Based on github.com/xkiian/cloudflare-jsd/visitors/
//
// 5-pass pipeline:
//   1. UnrollMaps -- inline numeric object properties
//   2. SequenceUnroller -- split comma expressions
//   3. ReplaceReassignments -- resolve alias chains, find string decoder
//   4. ReplaceStrings -- rotate string array, replace decoder calls
//   5. ConcatStrings -- fold "a" + "b" -> "ab"
//
// Then extract: ve (version "b"/"g"), path ("/jsd/oneshot/..."), alphabet (65-char LZ key)

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/t14raptor/go-fast/ast"
	"github.com/t14raptor/go-fast/generator"
	"github.com/t14raptor/go-fast/parser"
	"github.com/t14raptor/go-fast/transform/simplifier"
)

// Deobfuscate parses and deobfuscates a Cloudflare JSD script, extracting key parameters.
func Deobfuscate(src string) (*DeobfuscateResult, error) {
	prog, err := parser.ParseFile(src)
	if err != nil {
		return nil, fmt.Errorf("parse JS: %w", err)
	}

	unrollMaps(prog)
	sequenceUnroller(prog)
	callee := replaceReassignments(prog)
	if callee != nil {
		replaceStrings(prog, callee)
	}
	concatStrings(prog)
	simplifier.Simplify(prog, false)

	result := extractParams(prog)
	if result.Alphabet == "" {
		return nil, fmt.Errorf("failed to extract LZ alphabet from script")
	}
	if result.Path == "" {
		return nil, fmt.Errorf("failed to extract JSD path from script")
	}

	return result, nil
}

// DeobfuscateAndDump is like Deobfuscate but also writes the deobfuscated JS to a file for debugging.
func DeobfuscateAndDump(src string, outPath string) (*DeobfuscateResult, error) {
	prog, err := parser.ParseFile(src)
	if err != nil {
		return nil, fmt.Errorf("parse JS: %w", err)
	}

	unrollMaps(prog)
	sequenceUnroller(prog)
	callee := replaceReassignments(prog)
	if callee != nil {
		replaceStrings(prog, callee)
	}
	concatStrings(prog)
	simplifier.Simplify(prog, false)

	if outPath != "" {
		os.WriteFile(outPath, []byte(generator.Generate(prog)), 0644)
	}

	result := extractParams(prog)
	if result.Alphabet == "" {
		return nil, fmt.Errorf("failed to extract LZ alphabet from script")
	}
	if result.Path == "" {
		return nil, fmt.Errorf("failed to extract JSD path from script")
	}

	return result, nil
}

// --- Pass 1: UnrollMaps ---
// Inlines numeric object property lookups.

type mapUnroller struct {
	ast.NoopVisitor
	numberMap map[ast.Id]map[string]float64
}

func (v *mapUnroller) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	switch e := n.Expr.(type) {
	case *ast.AssignExpression:
		if e.Operator.String() != "=" {
			return
		}
		obj, ok := e.Right.Expr.(*ast.ObjectLiteral)
		if !ok {
			return
		}
		left, ok := e.Left.Expr.(*ast.Identifier)
		if !ok {
			return
		}
		for _, entry := range obj.Value {
			prop, ok := entry.Prop.(*ast.PropertyKeyed)
			if !ok {
				return
			}
			numLit, ok := prop.Value.Expr.(*ast.NumberLiteral)
			if !ok {
				continue
			}
			strLit, ok := prop.Key.Expr.(*ast.StringLiteral)
			if !ok {
				continue
			}
			id := left.ToId()
			if v.numberMap[id] == nil {
				v.numberMap[id] = make(map[string]float64)
			}
			v.numberMap[id][strLit.Value] = numLit.Value
			n.Expr = &ast.BooleanLiteral{Value: true}
		}
	case *ast.MemberExpression:
		id, ok := e.Object.Expr.(*ast.Identifier)
		if !ok {
			return
		}
		prop, ok := e.Property.Prop.(*ast.Identifier)
		if !ok {
			return
		}
		propMap := v.numberMap[id.ToId()]
		if propMap == nil {
			return
		}
		val := propMap[prop.Name]
		n.Expr = &ast.NumberLiteral{Value: val}
	}
}

func unrollMaps(p *ast.Program) {
	f := &mapUnroller{numberMap: make(map[ast.Id]map[string]float64)}
	f.V = f
	p.VisitWith(f)
}

// --- Pass 2: SequenceUnroller ---
// Splits comma expressions into separate statements.

type seqUnroller struct {
	ast.NoopVisitor
	stmts *ast.Statements
	index int
}

func (v *seqUnroller) insert(n int, seq ast.Expressions, trimLast bool) {
	if trimLast {
		n--
		seq = seq[:len(seq)-1]
	}
	newStmts := make(ast.Statements, len(*v.stmts)+n)
	copy(newStmts[:v.index], (*v.stmts)[:v.index])
	copy(newStmts[v.index+n:], (*v.stmts)[v.index:])
	for i := range seq {
		newStmts[v.index+i].Stmt = &ast.ExpressionStatement{Expression: &seq[i]}
	}
	v.index += n
	*v.stmts = newStmts
}

func (v *seqUnroller) VisitStatements(n *ast.Statements) {
	parent, parentIndex := v.stmts, v.index
	v.stmts = n
	for v.index = 0; v.index < len(*v.stmts); v.index++ {
		(*v.stmts)[v.index].VisitWith(v)
	}
	v.stmts, v.index = parent, parentIndex
}

func (v *seqUnroller) VisitExpressionStatement(n *ast.ExpressionStatement) {
	n.VisitChildrenWith(v)
	switch expr := n.Expression.Expr.(type) {
	case *ast.SequenceExpression:
		v.insert(len(expr.Sequence)-1, expr.Sequence, false)
	case *ast.AssignExpression:
		if seq, ok := expr.Right.Expr.(*ast.SequenceExpression); ok {
			expr.Right = &seq.Sequence[len(seq.Sequence)-1]
			v.insert(len(seq.Sequence), seq.Sequence, true)
		}
	}
}

func (v *seqUnroller) VisitThrowStatement(n *ast.ThrowStatement) {
	n.VisitChildrenWith(v)
	if seq, ok := n.Argument.Expr.(*ast.SequenceExpression); ok {
		n.Argument = &seq.Sequence[len(seq.Sequence)-1]
		v.insert(len(seq.Sequence), seq.Sequence, true)
	}
}

func (v *seqUnroller) VisitSwitchStatement(n *ast.SwitchStatement) {
	n.VisitChildrenWith(v)
	if seq, ok := n.Discriminant.Expr.(*ast.SequenceExpression); ok {
		n.Discriminant = &seq.Sequence[len(seq.Sequence)-1]
		v.insert(len(seq.Sequence), seq.Sequence, true)
	}
}

func (v *seqUnroller) VisitReturnStatement(n *ast.ReturnStatement) {
	n.VisitChildrenWith(v)
	if n.Argument == nil {
		return
	}
	if seq, ok := n.Argument.Expr.(*ast.SequenceExpression); ok {
		n.Argument = &seq.Sequence[len(seq.Sequence)-1]
		v.insert(len(seq.Sequence), seq.Sequence, true)
	}
}

func (v *seqUnroller) VisitIfStatement(n *ast.IfStatement) {
	n.VisitChildrenWith(v)
	if seq, ok := n.Test.Expr.(*ast.SequenceExpression); ok {
		n.Test = &seq.Sequence[len(seq.Sequence)-1]
		v.insert(len(seq.Sequence), seq.Sequence, true)
	}
}

func sequenceUnroller(p *ast.Program) {
	f := &seqUnroller{}
	f.V = f
	p.VisitWith(f)
}

// --- Pass 3: ReplaceReassignments ---
// Resolves variable alias chains and finds the most-called function (string decoder).

type gatherDecls struct {
	ast.NoopVisitor
	Decls map[ast.Id]*ast.Identifier
}

func (v *gatherDecls) VisitAssignExpression(n *ast.AssignExpression) {
	n.VisitChildrenWith(v)
	if n.Right == nil || n.Left == nil {
		return
	}
	rid, ok := n.Right.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	lid, ok := n.Left.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	if lid.ToId() == rid.ToId() {
		return
	}
	v.Decls[lid.ToId()] = rid
}

func resolveRoot(decls map[ast.Id]*ast.Identifier, id *ast.Identifier) *ast.Identifier {
	if id == nil {
		return nil
	}
	seen := make(map[ast.Id]struct{}, 8)
	cur := id
	for {
		cid := cur.ToId()
		if _, ok := seen[cid]; ok {
			return id
		}
		seen[cid] = struct{}{}
		nxt := decls[cid]
		if nxt == nil {
			return cur
		}
		cur = nxt
	}
}

func collapseReassignments(decls map[ast.Id]*ast.Identifier) {
	for k, v := range decls {
		decls[k] = resolveRoot(decls, v)
	}
}

type replaceIds struct {
	ast.NoopVisitor
	decls map[ast.Id]*ast.Identifier
}

func (v *replaceIds) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	id, ok := n.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	val := v.decls[id.ToId()]
	if val == nil {
		return
	}
	n.Expr = val
}

type callCounter struct {
	ast.NoopVisitor
	decls  map[ast.Id]*ast.Identifier
	counts map[ast.Id]int
	repr   map[ast.Id]*ast.Identifier
}

func (v *callCounter) VisitCallExpression(n *ast.CallExpression) {
	n.VisitChildrenWith(v)
	id, ok := n.Callee.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	root := resolveRoot(v.decls, id)
	if root == nil {
		return
	}
	rid := root.ToId()
	v.counts[rid]++
	if v.repr[rid] == nil {
		v.repr[rid] = root
	}
}

func replaceReassignments(p *ast.Program) *ast.Identifier {
	g := &gatherDecls{Decls: make(map[ast.Id]*ast.Identifier)}
	g.V = g
	p.VisitWith(g)

	collapseReassignments(g.Decls)

	cc := &callCounter{
		decls:  g.Decls,
		counts: make(map[ast.Id]int),
		repr:   make(map[ast.Id]*ast.Identifier),
	}
	cc.V = cc
	p.VisitWith(cc)

	r := &replaceIds{decls: g.Decls}
	r.V = r
	p.VisitWith(r)

	var best ast.Id
	bestN := 0
	for id, n := range cc.counts {
		if n > bestN {
			bestN = n
			best = id
		}
	}
	return cc.repr[best]
}

// --- Pass 4: ReplaceStrings ---
// Rotates the string array and replaces decoder function calls with literal strings.

type stringGatherer struct {
	ast.NoopVisitor
	funcName    string
	offset      int
	stringArray []string
	shuffleExpr *ast.Statement
}

func (v *stringGatherer) VisitCallExpression(n *ast.CallExpression) {
	n.VisitChildrenWith(v)
	callee, ok := n.Callee.Expr.(*ast.MemberExpression)
	if !ok {
		return
	}
	obj, ok := callee.Object.Expr.(*ast.StringLiteral)
	if !ok {
		return
	}
	if len(obj.Value) < 900 {
		return
	}
	v.stringArray = strings.Split(obj.Value, ",")
}

func (v *stringGatherer) VisitFunctionDeclaration(n *ast.FunctionDeclaration) {
	n.VisitChildrenWith(v)
	if n.Function.Name.Name != v.funcName {
		return
	}
	eStmt, ok := n.Function.Body.List[0].Stmt.(*ast.ExpressionStatement)
	if !ok {
		return
	}
	aExpr, ok := eStmt.Expression.Expr.(*ast.AssignExpression)
	if !ok {
		return
	}
	binExpr, ok := aExpr.Right.Expr.(*ast.BinaryExpression)
	if !ok {
		return
	}
	right, ok := binExpr.Right.Expr.(*ast.NumberLiteral)
	if !ok {
		return
	}
	val := right.Value
	if binExpr.Operator.String() == "-" {
		val *= -1
	}
	v.offset = int(val)
}

func (v *stringGatherer) VisitForStatement(n *ast.ForStatement) {
	n.VisitChildrenWith(v)
	if _, ok := n.Body.Stmt.(*ast.TryStatement); !ok {
		return
	}
	if !strings.Contains(generator.Generate(n), "parseInt") {
		return
	}
	v.shuffleExpr = n.Body
}

type stringReplacer struct {
	ast.NoopVisitor
	funcName    string
	offset      int
	stringArray []string
}

func (v *stringReplacer) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	callExpr, ok := n.Expr.(*ast.CallExpression)
	if !ok {
		return
	}
	identifier, ok := callExpr.Callee.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	if identifier.Name != v.funcName {
		return
	}
	if len(callExpr.ArgumentList) != 1 {
		return
	}
	numExpr, ok := callExpr.ArgumentList[0].Expr.(*ast.NumberLiteral)
	if !ok {
		return
	}
	idx := int(numExpr.Value) + v.offset
	if idx >= 0 && idx < len(v.stringArray) {
		n.Expr = &ast.StringLiteral{Value: v.stringArray[idx]}
	}
}

var normalRe = regexp.MustCompile(`^[0-9][a-zA-Z0-9+\-*/%()=<>!&|^.,\s]*$`)
var shuffleCheckerRe = regexp.MustCompile(`parseInt\(.\((\d*?)\)\)`)

func replaceStrings(p *ast.Program, id *ast.Identifier) {
	f := &stringGatherer{funcName: id.Name}
	f.V = f
	p.VisitWith(f)

	if f.shuffleExpr == nil || len(f.stringArray) == 0 {
		return
	}

	matches := shuffleCheckerRe.FindAllStringSubmatch(generator.Generate(f.shuffleExpr), -1)
	var out []int
	for _, m := range matches {
		val, err := strconv.ParseInt(m[1], 0, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse error:", err)
			continue
		}
		out = append(out, int(val))
	}

outer:
	for {
		for _, entry := range out {
			idx := entry + f.offset
			if idx < 0 || idx >= len(f.stringArray) {
				f.stringArray = append(f.stringArray[1:], f.stringArray[0])
				continue outer
			}
			text := f.stringArray[idx]
			if !normalRe.MatchString(text) {
				f.stringArray = append(f.stringArray[1:], f.stringArray[0])
				continue outer
			}
		}
		break
	}

	f2 := &stringReplacer{
		funcName:    id.Name,
		offset:      f.offset,
		stringArray: f.stringArray,
	}
	f2.V = f2
	p.VisitWith(f2)
}

// --- Pass 5: ConcatStrings ---
// Folds string concatenation: "a" + "b" -> "ab".

type strConcat struct {
	ast.NoopVisitor
}

func (v *strConcat) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	binExpr, ok := n.Expr.(*ast.BinaryExpression)
	if !ok {
		return
	}
	leftLit, ok := binExpr.Left.Expr.(*ast.StringLiteral)
	if !ok {
		return
	}
	rightLit, ok := binExpr.Right.Expr.(*ast.StringLiteral)
	if !ok {
		return
	}
	n.Expr = &ast.StringLiteral{Value: leftLit.Value + rightLit.Value}
}

func concatStrings(p *ast.Program) {
	f := &strConcat{}
	f.V = f
	p.VisitWith(f)
}

// --- Extractor ---
// Walks the deobfuscated AST to find ve, path, and alphabet.

const lzStringURISafeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+-$"

func isExactAlphabetPermutation(s string) bool {
	if len(s) != len(lzStringURISafeAlphabet) {
		return false
	}
	var allowed [256]bool
	for i := 0; i < len(lzStringURISafeAlphabet); i++ {
		allowed[lzStringURISafeAlphabet[i]] = true
	}
	var seen [256]bool
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !allowed[b] || seen[b] {
			return false
		}
		seen[b] = true
	}
	return true
}

type paramExtractor struct {
	ast.NoopVisitor
	result *DeobfuscateResult
}

func (v *paramExtractor) VisitStringLiteral(n *ast.StringLiteral) {
	n.VisitChildrenWith(v)
	val := n.Value
	if strings.HasPrefix(val, "/jsd/oneshot/") {
		v.result.Path = val
	} else if isExactAlphabetPermutation(val) {
		v.result.Alphabet = val
	}
}

func (v *paramExtractor) VisitObjectLiteral(n *ast.ObjectLiteral) {
	n.VisitChildrenWith(v)
	if len(n.Value) != 1 {
		return
	}
	prop, ok := n.Value[0].Prop.(*ast.PropertyKeyed)
	if !ok {
		return
	}
	strLit, ok := prop.Value.Expr.(*ast.StringLiteral)
	if !ok {
		return
	}
	if strLit.Value == "b" || strLit.Value == "g" {
		v.result.Ve = strLit.Value
	}
}

func extractParams(p *ast.Program) *DeobfuscateResult {
	f := &paramExtractor{result: &DeobfuscateResult{}}
	f.V = f
	p.VisitWith(f)
	return f.result
}
