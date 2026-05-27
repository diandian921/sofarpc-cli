package javaparser

import "strings"

// preamble 是 type / method / field 声明前的 modifier + annotation + javadoc 集合。
// 单独抽出来是因为三种 declaration 都用同样的 preamble 形态。
type preamble struct {
	Modifiers   []string
	Annotations []Annotation
	Javadoc     string
}

// parsePreamble 在当前位置消费 0+ 个 modifier / annotation,并取上方紧邻的 javadoc。
// 修饰符包括:
//   - Java 关键字 modifier:public / private / protected / static / final / abstract /
//     default / synchronized / native / transient / volatile / strictfp / sealed
//   - 非 Java 关键字但出现在 modifier 位置的:`non-sealed`(由 `non` + `-` + `sealed` 三 token 合成)
//
// **不消费** type 关键字(class / interface / enum / record / @interface)。
// 退出条件:peek 不是 modifier / annotation。
func parsePreamble(c *cursor) (preamble, error) {
	p := preamble{Javadoc: cleanJavadocText(c.peekJavadoc())}
	for {
		tok := c.peek()
		// annotation
		if tok.Kind == TokenAt {
			// 但要排除 `@interface`(annotation declaration 的 type keyword)
			if c.peekN(1).Kind == TokenKeyword && c.peekN(1).Value == "interface" {
				return p, nil
			}
			ann, err := parseAnnotation(c)
			if err != nil {
				return p, err
			}
			p.Annotations = append(p.Annotations, ann)
			continue
		}
		// non-sealed:三 token 合成,但要求三段在源文件中**相邻**(无空格)。
		// codex review #6:lexer 丢弃空格,光检查 token kind 会把 `non - sealed`
		// (有空格,非法 Java)也合并 → 用 Token.Off 校验相邻性。
		if tok.Kind == TokenIdent && tok.Value == "non" {
			dash := c.peekN(1)
			seal := c.peekN(2)
			if dash.Kind == TokenOther && dash.Value == "-" &&
				seal.Kind == TokenKeyword && seal.Value == "sealed" &&
				tok.Off+len("non") == dash.Off && dash.Off+1 == seal.Off {
				c.consume()
				c.consume()
				c.consume()
				p.Modifiers = append(p.Modifiers, "non-sealed")
				continue
			}
		}
		// keyword modifier
		if tok.Kind == TokenKeyword && isModifierKeyword(tok.Value) {
			c.consume()
			p.Modifiers = append(p.Modifiers, tok.Value)
			continue
		}
		return p, nil
	}
}

func isModifierKeyword(kw string) bool {
	switch kw {
	case "public", "private", "protected",
		"static", "final", "abstract", "default",
		"synchronized", "native", "transient", "volatile",
		"strictfp", "sealed":
		return true
	}
	return false
}

// parseAnnotation 解析 @Name 或 @Name(...) 或 @a.b.Name(...);只记 Name,不解析 args。
// Annotation 名段允许 contextual keyword(codex review #2)。
func parseAnnotation(c *cursor) (Annotation, error) {
	startPos := c.pos()
	if _, err := c.expect(TokenAt, "@"); err != nil {
		return Annotation{}, err
	}
	first := c.peek()
	if !isIdentLike(first) {
		return Annotation{}, parseError(tokenPos(first), "expected annotation name, got %s %q", first.Kind, first.Value)
	}
	c.consume()
	name := first.Value
	for c.peek().Kind == TokenDot && isIdentLike(c.peekN(1)) {
		c.consume() // dot
		next := c.consume()
		name += "." + next.Value
	}
	if c.peek().Kind == TokenLParen {
		if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
			return Annotation{}, err
		}
	}
	return Annotation{Name: name, Pos: startPos}, nil
}

// parseTypeDecl 解析一个顶层或嵌套 type declaration。
// 调用前提:c.peek() 是 modifier / annotation / 一个 type keyword(class/interface/enum/record/@interface)。
// 失败时返回 error;成功消费完整 type body(含尾部 `}`)并返回 TypeDecl。
func parseTypeDecl(c *cursor) (TypeDecl, error) {
	pre, err := parsePreamble(c)
	if err != nil {
		return TypeDecl{}, err
	}

	startPos := c.pos()
	tok := c.peek()

	// 识别 type kind
	var kind TypeKind
	switch {
	case tok.Kind == TokenKeyword && tok.Value == "class":
		kind = TypeKindClass
		c.consume()
	case tok.Kind == TokenKeyword && tok.Value == "interface":
		kind = TypeKindInterface
		c.consume()
	case tok.Kind == TokenKeyword && tok.Value == "enum":
		kind = TypeKindEnum
		c.consume()
	case tok.Kind == TokenKeyword && tok.Value == "record":
		kind = TypeKindRecord
		c.consume()
	case tok.Kind == TokenAt && c.peekN(1).Kind == TokenKeyword && c.peekN(1).Value == "interface":
		kind = TypeKindAnnotation
		c.consume() // @
		c.consume() // interface
	default:
		return TypeDecl{}, parseError(startPos, "expected type keyword (class/interface/enum/record/@interface), got %s %q", tok.Kind, tok.Value)
	}

	nameTok, err := expectIdentLike(c, "type name")
	if err != nil {
		return TypeDecl{}, err
	}

	decl := TypeDecl{
		Kind:        kind,
		Modifiers:   pre.Modifiers,
		Annotations: pre.Annotations,
		Javadoc:     pre.Javadoc,
		Name:        nameTok.Value,
		Pos:         startPos,
	}

	// declared type params
	tparams, err := parseTypeParams(c)
	if err != nil {
		return TypeDecl{}, err
	}
	decl.TypeParams = tparams

	// record header(必须紧跟 type params 之后,在 extends/implements 之前)
	if kind == TypeKindRecord {
		if c.peek().Kind != TokenLParen {
			return TypeDecl{}, parseError(c.pos(), "record %s missing header parameter list", decl.Name)
		}
		comps, err := parseRecordHeader(c)
		if err != nil {
			return TypeDecl{}, err
		}
		decl.RecordComponents = comps
	}

	// extends / implements / permits
	for {
		tok := c.peek()
		if tok.Kind != TokenKeyword {
			break
		}
		switch tok.Value {
		case "extends":
			c.consume()
			refs, err := parseTypeRefList(c)
			if err != nil {
				return TypeDecl{}, err
			}
			decl.Extends = refs
		case "implements":
			c.consume()
			refs, err := parseTypeRefList(c)
			if err != nil {
				return TypeDecl{}, err
			}
			decl.Implements = refs
		case "permits":
			c.consume()
			refs, err := parseTypeRefList(c)
			if err != nil {
				return TypeDecl{}, err
			}
			decl.Permits = refs
		default:
			goto bodyStart
		}
	}
bodyStart:

	// Enter body
	if _, err := c.expect(TokenLBrace, "{"); err != nil {
		return TypeDecl{}, err
	}
	if err := parseTypeBody(c, &decl); err != nil {
		return TypeDecl{}, err
	}
	if _, err := c.expect(TokenRBrace, "}"); err != nil {
		return TypeDecl{}, err
	}
	return decl, nil
}

// parseTypeRefList 解析逗号分隔的 TypeRef 序列(用于 extends/implements/permits/throws)。
// 至少 1 个。
func parseTypeRefList(c *cursor) ([]TypeRef, error) {
	var refs []TypeRef
	for {
		ref, err := parseTypeRef(c)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
		if !c.match(TokenComma) {
			return refs, nil
		}
	}
}

// parseTypeBody 在已消费 `{` 之后、消费 `}` 之前,遍历 type body 内全部成员。
// 不消费 trailing `}`,留给 caller。
// kind 分支:
//   - enum/annotation:走自己的 parser(Task 9 接入),此处只 brace-skip 占位
//   - class/interface/record:走 member dispatch
func parseTypeBody(c *cursor, decl *TypeDecl) error {
	if decl.Kind == TypeKindEnum || decl.Kind == TypeKindAnnotation {
		return skipUntilMatchingRBrace(c)
	}
	for {
		if c.peek().Kind == TokenSemicolon {
			c.consume()
			continue
		}
		if c.peek().Kind == TokenRBrace || c.eof() {
			return nil
		}
		if err := parseMember(c, decl); err != nil {
			return err
		}
	}
}

// skipUntilMatchingRBrace 平衡 skip 到外层 RBrace 之前(不消费 RBrace)。
// 用于 Task 6 阶段把 enum/annotation body 整段 skip(Task 9 替换)。
func skipUntilMatchingRBrace(c *cursor) error {
	depth := 0
	for !c.eof() {
		tok := c.peek()
		if tok.Kind == TokenLBrace {
			depth++
			c.consume()
			continue
		}
		if tok.Kind == TokenRBrace {
			if depth == 0 {
				return nil
			}
			depth--
			c.consume()
			continue
		}
		c.consume()
	}
	return parseError(c.pos(), "unexpected EOF in body")
}

// parseMember 解析单个 type body 成员。 调用前提:peek 不是 RBrace 也不是 Semicolon。
//
// Dispatch 顺序:
//  1. peek 是 type keyword 或 `@interface` → nested type(无 preamble)
//  2. 否则消费 preamble,再判断是 nested type / initializer block / method / field
func parseMember(c *cursor, owner *TypeDecl) error {
	if peekIsNestedTypeStart(c) {
		nested, err := parseTypeDecl(c)
		if err != nil {
			return err
		}
		owner.NestedTypes = append(owner.NestedTypes, nested)
		return nil
	}

	pre, err := parsePreamble(c)
	if err != nil {
		return err
	}

	if peekIsNestedTypeStart(c) {
		nested, err := parseTypeDeclWithPreamble(c, pre)
		if err != nil {
			return err
		}
		owner.NestedTypes = append(owner.NestedTypes, nested)
		return nil
	}

	// initializer block:peek 是 `{`(可能前面有 `static` modifier;也可能完全 anonymous)
	if c.peek().Kind == TokenLBrace {
		return c.skipBalanced(TokenLBrace, TokenRBrace)
	}

	mdecl, fdecls, err := parseMethodOrField(c, pre, owner)
	if err != nil {
		return err
	}
	if mdecl != nil {
		owner.Methods = append(owner.Methods, *mdecl)
	}
	owner.Fields = append(owner.Fields, fdecls...)
	return nil
}

// peekIsNestedTypeStart 当前位置直接是 type keyword(没有 modifier/annotation/javadoc 先吃)。
// `@interface` 也算。
func peekIsNestedTypeStart(c *cursor) bool {
	tok := c.peek()
	if tok.Kind == TokenKeyword {
		switch tok.Value {
		case "class", "interface", "enum", "record":
			return true
		}
	}
	if tok.Kind == TokenAt && c.peekN(1).Kind == TokenKeyword && c.peekN(1).Value == "interface" {
		return true
	}
	return false
}

// parseTypeDeclWithPreamble 复用 parseTypeDecl 流程,但 preamble 已经在外部消费。
// 实现:把 pre 写进 decl 字段,然后从 type keyword 开始走 parseTypeDecl body 部分。
func parseTypeDeclWithPreamble(c *cursor, pre preamble) (TypeDecl, error) {
	startPos := c.pos()
	tok := c.peek()
	var kind TypeKind
	switch {
	case tok.Kind == TokenKeyword && tok.Value == "class":
		kind = TypeKindClass
		c.consume()
	case tok.Kind == TokenKeyword && tok.Value == "interface":
		kind = TypeKindInterface
		c.consume()
	case tok.Kind == TokenKeyword && tok.Value == "enum":
		kind = TypeKindEnum
		c.consume()
	case tok.Kind == TokenKeyword && tok.Value == "record":
		kind = TypeKindRecord
		c.consume()
	case tok.Kind == TokenAt:
		kind = TypeKindAnnotation
		c.consume()
		c.consume()
	default:
		return TypeDecl{}, parseError(startPos, "expected nested type keyword, got %s %q", tok.Kind, tok.Value)
	}
	nameTok, err := expectIdentLike(c, "type name")
	if err != nil {
		return TypeDecl{}, err
	}
	decl := TypeDecl{
		Kind:        kind,
		Modifiers:   pre.Modifiers,
		Annotations: pre.Annotations,
		Javadoc:     pre.Javadoc,
		Name:        nameTok.Value,
		Pos:         startPos,
	}
	tparams, err := parseTypeParams(c)
	if err != nil {
		return TypeDecl{}, err
	}
	decl.TypeParams = tparams
	if kind == TypeKindRecord {
		if c.peek().Kind != TokenLParen {
			return TypeDecl{}, parseError(c.pos(), "record %s missing header", decl.Name)
		}
		comps, err := parseRecordHeader(c)
		if err != nil {
			return TypeDecl{}, err
		}
		decl.RecordComponents = comps
	}
	for {
		tok := c.peek()
		if tok.Kind != TokenKeyword {
			break
		}
		switch tok.Value {
		case "extends":
			c.consume()
			refs, err := parseTypeRefList(c)
			if err != nil {
				return TypeDecl{}, err
			}
			decl.Extends = refs
		case "implements":
			c.consume()
			refs, err := parseTypeRefList(c)
			if err != nil {
				return TypeDecl{}, err
			}
			decl.Implements = refs
		case "permits":
			c.consume()
			refs, err := parseTypeRefList(c)
			if err != nil {
				return TypeDecl{}, err
			}
			decl.Permits = refs
		default:
			goto bodyStart2
		}
	}
bodyStart2:
	if _, err := c.expect(TokenLBrace, "{"); err != nil {
		return TypeDecl{}, err
	}
	if err := parseTypeBody(c, &decl); err != nil {
		return TypeDecl{}, err
	}
	if _, err := c.expect(TokenRBrace, "}"); err != nil {
		return TypeDecl{}, err
	}
	return decl, nil
}

// parseMethodOrField 解析 type body 中的一个 method / field / ctor。
//
// 流程:
//
//	1. optional declared type params(generic method `<T> T foo(...)`)
//	2. 看是否 ctor:next token 是 Ident 且 value == owner.Name 且 peekN(1) == `(`
//	   → 直接走 method path,ReturnType 为空,IsConstructor = true
//	3. 否则:parseTypeRef → ReturnType
//	4. 接 Ident(member 名)
//	5. 若 peek 是 `(` → method;否则 → field(可能 multi-decl)
//
// 返回 (method, fields, err):method 与 fields 互斥,但有可能两者都是 nil
// (例如 `;` 单独占位但已被 parseTypeBody 提前剥掉)。
func parseMethodOrField(c *cursor, pre preamble, owner *TypeDecl) (*MethodDecl, []FieldDecl, error) {
	tparams, err := parseTypeParams(c)
	if err != nil {
		return nil, nil, err
	}

	// ctor 快路径:Ident(owner.Name) + `(`
	if tok := c.peek(); tok.Kind == TokenIdent && tok.Value == owner.Name && c.peekN(1).Kind == TokenLParen {
		ctor, err := parseConstructorDecl(c, pre, tparams)
		if err != nil {
			return nil, nil, err
		}
		return ctor, nil, nil
	}

	startPos := c.pos()
	retType, err := parseTypeRef(c)
	if err != nil {
		return nil, nil, err
	}

	nameTok, err := expectIdentLike(c, "method or field name")
	if err != nil {
		return nil, nil, err
	}

	if c.peek().Kind == TokenLParen {
		method, err := finishMethodDecl(c, pre, tparams, retType, nameTok.Value, startPos)
		if err != nil {
			return nil, nil, err
		}
		return &method, nil, nil
	}

	// field — Task 8 接入完整 multi-decl 逻辑;Task 7 先返回单字段(无 multi-decl 时正常 work)
	fields, err := finishFieldDecl(c, pre, retType, nameTok.Value, startPos)
	if err != nil {
		return nil, nil, err
	}
	return nil, fields, nil
}

func parseConstructorDecl(c *cursor, pre preamble, tparams []TypeParam) (*MethodDecl, error) {
	startPos := c.pos()
	nameTok, err := expectIdentLike(c, "ctor name")
	if err != nil {
		return nil, err
	}
	method := MethodDecl{
		Modifiers:     pre.Modifiers,
		Annotations:   pre.Annotations,
		Javadoc:       pre.Javadoc,
		TypeParams:    tparams,
		Name:          nameTok.Value,
		IsConstructor: true,
		Pos:           startPos,
	}
	params, err := parseParamList(c)
	if err != nil {
		return nil, err
	}
	method.Params = params
	if c.matchKeyword("throws") {
		refs, err := parseTypeRefList(c)
		if err != nil {
			return nil, err
		}
		method.Throws = refs
	}
	// ctor body 必有 `{ ... }`,skip
	if c.peek().Kind == TokenLBrace {
		if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
			return nil, err
		}
	} else if c.peek().Kind == TokenSemicolon {
		c.consume()
	} else {
		return nil, parseError(c.pos(), "ctor missing body or `;`")
	}
	return &method, nil
}

func finishMethodDecl(c *cursor, pre preamble, tparams []TypeParam, retType TypeRef, name string, startPos Position) (MethodDecl, error) {
	method := MethodDecl{
		Modifiers:   pre.Modifiers,
		Annotations: pre.Annotations,
		Javadoc:     pre.Javadoc,
		TypeParams:  tparams,
		ReturnType:  retType,
		Name:        name,
		Pos:         startPos,
	}
	params, err := parseParamList(c)
	if err != nil {
		return method, err
	}
	method.Params = params

	// C-style return-type array suffix:`int foo()[]` 形态。 JLS 允许,parser 容错。
	// 把它累加到 ReturnType.ArrayDims。
	if c.peek().Kind == TokenLBracket {
		extra := readArrayDims(c)
		method.ReturnType.ArrayDims += extra
	}

	if c.matchKeyword("throws") {
		refs, err := parseTypeRefList(c)
		if err != nil {
			return method, err
		}
		method.Throws = refs
	}

	// annotation type `default <expr>` 在 Task 9 完整处理。 这里只识别 method body 或 `;`。
	switch c.peek().Kind {
	case TokenLBrace:
		if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
			return method, err
		}
	case TokenSemicolon:
		c.consume()
	default:
		// 容错:可能是 annotation `default literal;`,Task 9 替换
		if c.peek().Kind == TokenKeyword && c.peek().Value == "default" {
			if !c.skipUntil(TokenSemicolon) {
				return method, parseError(c.pos(), "annotation method `default` missing trailing `;`")
			}
			c.consume()
			return method, nil
		}
		return method, parseError(c.pos(), "method %s missing body or `;`, got %s %q", name, c.peek().Kind, c.peek().Value)
	}
	return method, nil
}

// parseParamList 解析 `( param, param, ... )`。 必须以 `(` 开头,以 `)` 结尾。
// 允许空 list。 支持 varargs(`T... name`)。
func parseParamList(c *cursor) ([]ParamDecl, error) {
	if _, err := c.expect(TokenLParen, "("); err != nil {
		return nil, err
	}
	var params []ParamDecl
	if c.peek().Kind == TokenRParen {
		c.consume()
		return params, nil
	}
	for {
		p, err := parseSingleParam(c)
		if err != nil {
			return nil, err
		}
		params = append(params, p)
		if c.match(TokenComma) {
			continue
		}
		break
	}
	if _, err := c.expect(TokenRParen, ")"); err != nil {
		return nil, err
	}
	return params, nil
}

func parseSingleParam(c *cursor) (ParamDecl, error) {
	p := ParamDecl{}
	// annotations + final
	for {
		tok := c.peek()
		if tok.Kind == TokenAt {
			ann, err := parseAnnotation(c)
			if err != nil {
				return p, err
			}
			p.Annotations = append(p.Annotations, ann)
			continue
		}
		if tok.Kind == TokenKeyword && tok.Value == "final" {
			c.consume()
			p.Final = true
			continue
		}
		break
	}
	// type
	t, err := parseTypeRef(c)
	if err != nil {
		return p, err
	}
	// varargs:`T... name`(C.1 lexer 已经把 `...` 合成 TokenEllipsis)
	if c.match(TokenEllipsis) {
		p.IsVarargs = true
		t.ArrayDims++
	}
	p.Type = t
	// name
	nameTok, err := expectIdentLike(c, "parameter name")
	if err != nil {
		return p, err
	}
	p.Name = nameTok.Value
	// C-style array dim 在 name 之后:`int a[]` → 把 dim 加回 Type
	if c.peek().Kind == TokenLBracket {
		extra := readArrayDims(c)
		p.Type.ArrayDims += extra
	}
	return p, nil
}

// finishFieldDecl 处理一个 field declaration,允许 multi-decl(`int a, b = 1, c[];`)。
// 输入:已经消费完 type 和 第一个 name,startPos 是 type 位置。
// 输出:展开成多个 FieldDecl,每个 share modifiers/annotations/javadoc/type(type 可能因 per-name `[]` 调整 ArrayDims)。
func finishFieldDecl(c *cursor, pre preamble, typ TypeRef, firstName string, startPos Position) ([]FieldDecl, error) {
	var fields []FieldDecl
	curName := firstName
	curType := typ
	if c.peek().Kind == TokenLBracket {
		curType.ArrayDims += readArrayDims(c)
	}
	if c.match(TokenAssign) {
		if err := skipFieldInitializer(c); err != nil {
			return nil, err
		}
	}
	fields = append(fields, FieldDecl{
		Modifiers:   pre.Modifiers,
		Annotations: pre.Annotations,
		Javadoc:     pre.Javadoc,
		Type:        curType,
		Name:        curName,
		Pos:         startPos,
	})
	for c.match(TokenComma) {
		curType = typ
		nameTok, err := expectIdentLike(c, "field name in multi-declaration")
		if err != nil {
			return nil, err
		}
		curName = nameTok.Value
		if c.peek().Kind == TokenLBracket {
			curType.ArrayDims += readArrayDims(c)
		}
		if c.match(TokenAssign) {
			if err := skipFieldInitializer(c); err != nil {
				return nil, err
			}
		}
		fields = append(fields, FieldDecl{
			Modifiers:   pre.Modifiers,
			Annotations: pre.Annotations,
			Javadoc:     pre.Javadoc,
			Type:        curType,
			Name:        curName,
			Pos:         startPos,
		})
	}
	if _, err := c.expect(TokenSemicolon, ";"); err != nil {
		return nil, err
	}
	return fields, nil
}

// skipFieldInitializer 从 `=` 之后(已消费)跳到下一个顶层 `;` 或 `,`(留给 caller 看)。
// 内部正确平衡 () [] {}。 不消费目标分隔符。
func skipFieldInitializer(c *cursor) error {
	for !c.eof() {
		tok := c.peek()
		switch tok.Kind {
		case TokenSemicolon, TokenComma:
			return nil
		case TokenLParen:
			if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
				return err
			}
		case TokenLBracket:
			if err := c.skipBalanced(TokenLBracket, TokenRBracket); err != nil {
				return err
			}
		case TokenLBrace:
			if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
				return err
			}
		default:
			c.consume()
		}
	}
	return parseError(c.pos(), "field initializer hit EOF without `;`")
}

// parseRecordHeader Task 9 才实现,Task 5 阶段只 brace-skip 占位以让 record decl 整体可解析。
func parseRecordHeader(c *cursor) ([]ParamDecl, error) {
	if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
		return nil, err
	}
	return nil, nil
}

// cleanJavadocText 把 `/** ... */` 原文(含 javadoc 注释符)清洗成纯文本。
// 复用 schema 包 cleanJavadoc 的策略:去 `/**` / `*/` 包围,行首 `*` 去除,
// 跳过 `@tag` 行,行内空白合并。
func cleanJavadocText(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/**")
	raw = strings.TrimSuffix(raw, "*/")
	lines := strings.Split(raw, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line != "" && !strings.HasPrefix(line, "@") {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}
