package javaparser

import (
	"fmt"
	"strings"
)

// Parse 把 Java 源代码解析为 CompilationUnit AST。
// 输入是任意 UTF-8 bytes + 源文件路径(用于 AST SourceFile 字段)。
// 返回的 AST 不做 import 解析、不做 type FQN 解析 —— 那是 C.3 adapter 的事。
//
// 错误分类:
//   - lexer 错误(unterminated comment/string/char/text block)直接透传
//   - parser 错误统一格式 "parse error at L:C: <reason>"
func Parse(src []byte, sourceFile string) (*CompilationUnit, error) {
	tokens, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	c := newCursor(tokens)
	return parseCompilationUnit(c, sourceFile)
}

func parseCompilationUnit(c *cursor, sourceFile string) (*CompilationUnit, error) {
	cu := &CompilationUnit{SourceFile: sourceFile}

	// 顺序(JLS):file-level annotation? package? imports? types?
	// 容错解析:`@` 在 file 层有歧义 —— 可能是 file-level annotation(后接 `package`),
	// 也可能是 type declaration 的 preamble(后接 modifier / type keyword)。
	// 用 peekAnnotationsLeadToKeyword 提前判断,避免吞掉 type-decl annotation(codex review #1)。
	for !c.eof() {
		tok := c.peek()

		// `@` + ... + `package`(可选 imports/types 之前)= file-level annotation,skip
		if tok.Kind == TokenAt && peekAnnotationsLeadToKeyword(c, "package") {
			if err := skipFileLevelAnnotation(c); err != nil {
				return nil, err
			}
			continue
		}

		if tok.Kind == TokenKeyword && tok.Value == "package" {
			pkg, err := parsePackage(c)
			if err != nil {
				return nil, err
			}
			cu.Package = pkg
			continue
		}
		if tok.Kind == TokenKeyword && tok.Value == "import" {
			imp, err := parseImport(c)
			if err != nil {
				return nil, err
			}
			cu.Imports = append(cu.Imports, imp)
			continue
		}
		// 任何剩余 token(含 type-decl annotation `@Foo public class X`)→ parseTypeDecl
		decl, err := parseTypeDecl(c)
		if err != nil {
			return nil, err
		}
		cu.Types = append(cu.Types, decl)
	}
	return cu, nil
}

// skipFileLevelAnnotation 跳过文件级 annotation(在 package 之前出现的 @SuppressWarnings 等)。
// 形态:`@Name` 或 `@Name(...)` 或 `@a.b.Name(...)`。
// Annotation 名段允许 contextual keyword(`record` / `sealed` / `var` 等 —— Java 真实
// 代码常出现包名 `com.acme.record.X`),不限定纯 TokenIdent。
func skipFileLevelAnnotation(c *cursor) error {
	if _, err := c.expect(TokenAt, "@"); err != nil {
		return err
	}
	for {
		tok := c.peek()
		if !isIdentLike(tok) {
			return parseError(tokenPos(tok), "expected annotation name, got %s %q", tok.Kind, tok.Value)
		}
		c.consume()
		if !c.match(TokenDot) {
			break
		}
	}
	if c.peek().Kind == TokenLParen {
		if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
			return err
		}
	}
	return nil
}

// peekAnnotationsLeadToKeyword 不消费 token,判断 `@...` annotation 序列之后
// 第一个非 annotation token 是否是给定 keyword。 用于 file-level annotation 与
// type-decl annotation 的歧义消除。
// 保存 c.idx,临时推进,return 前恢复。
func peekAnnotationsLeadToKeyword(c *cursor, kw string) bool {
	saved := c.idx
	defer func() { c.idx = saved }()
	for {
		c.skipTrivia()
		if c.idx >= len(c.tokens) {
			return false
		}
		tok := c.tokens[c.idx]
		if tok.Kind != TokenAt {
			return tok.Kind == TokenKeyword && tok.Value == kw
		}
		c.idx++ // @
		c.skipTrivia()
		// 吃 qualified annotation name(允许 contextual keyword)。
		// codex review (round 2) #1:必须至少看见 1 段 ident,否则视为 malformed →
		// 不当 file-level annotation,让 caller(主循环)落到 parseTypeDecl 路径报错。
		sawName := false
		for c.idx < len(c.tokens) && isIdentLike(c.tokens[c.idx]) {
			sawName = true
			c.idx++
			c.skipTrivia()
			if c.idx < len(c.tokens) && c.tokens[c.idx].Kind == TokenDot {
				c.idx++
				c.skipTrivia()
				continue
			}
			break
		}
		if !sawName {
			return false
		}
		// optional `(...)` balanced skip
		if c.idx < len(c.tokens) && c.tokens[c.idx].Kind == TokenLParen {
			depth := 1
			c.idx++
			for depth > 0 && c.idx < len(c.tokens) {
				switch c.tokens[c.idx].Kind {
				case TokenLParen:
					depth++
				case TokenRParen:
					depth--
				}
				c.idx++
			}
		}
	}
}

// isContextualKeyword 返回 Java 9+ / 14+ 中可作为标识符的 contextual keyword。
// 这些在 keywords.go 被识别成 TokenKeyword,但 JLS 允许它们出现在 non-keyword
// 位置(包名、类名段、变量名等)。
func isContextualKeyword(value string) bool {
	switch value {
	case "module", "open", "opens", "uses", "provides", "requires",
		"exports", "to", "with", "transitive",
		"record", "sealed", "permits", "yield", "var":
		return true
	}
	return false
}

// isIdentLike 返回 tok 是否可在标识符位置出现(普通 ident 或 contextual keyword)。
// 用于:dotted name、qualified type name、annotation 名段、import path、类名 / 方法名 /
// 字段名 / 参数名 / enum constant 名。
// codex review #2:Java 包名 `com.acme.record.UserDO` 真实存在,parser 不能拒绝。
// codex review (round 2) #6:类名 / 方法名 / 字段名等也允许 contextual keyword,
// 虽然罕见但合法(如 `class record { }` —— 不推荐但 JLS 允许)。
func isIdentLike(tok Token) bool {
	return tok.Kind == TokenIdent || (tok.Kind == TokenKeyword && isContextualKeyword(tok.Value))
}

// expectIdentLike 消费一个 ident-like token(普通 ident 或 contextual keyword)。
// 不匹配返回 parseError。 用作 `c.expect(TokenIdent, ...)` 在 declaration name
// 位置的扩展替换。
func expectIdentLike(c *cursor, what string) (Token, error) {
	tok := c.peek()
	if !isIdentLike(tok) {
		return tok, parseError(tokenPos(tok), "expected %s, got %s %q", what, tok.Kind, tok.Value)
	}
	return c.consume(), nil
}

func parsePackage(c *cursor) (*PackageDecl, error) {
	startPos := c.pos()
	if _, err := c.expectKeyword("package"); err != nil {
		return nil, err
	}
	name, err := readDottedName(c)
	if err != nil {
		return nil, err
	}
	if _, err := c.expect(TokenSemicolon, ";"); err != nil {
		return nil, err
	}
	return &PackageDecl{Name: name, Pos: startPos}, nil
}

func parseImport(c *cursor) (ImportDecl, error) {
	startPos := c.pos()
	if _, err := c.expectKeyword("import"); err != nil {
		return ImportDecl{}, err
	}
	imp := ImportDecl{Pos: startPos}
	if c.matchKeyword("static") {
		imp.Static = true
	}
	first := c.peek()
	if !isIdentLike(first) {
		return ImportDecl{}, parseError(tokenPos(first), "expected import path, got %s %q", first.Kind, first.Value)
	}
	c.consume()
	var parts []string
	parts = append(parts, first.Value)
	for c.match(TokenDot) {
		// 看下一个 token:ident-like 继续拼,Star 表示 wildcard 结束
		nxt := c.peek()
		if nxt.Kind == TokenStar {
			c.consume()
			imp.Wildcard = true
			break
		}
		if !isIdentLike(nxt) {
			return ImportDecl{}, parseError(tokenPos(nxt), "expected identifier or *, got %s %q", nxt.Kind, nxt.Value)
		}
		c.consume()
		parts = append(parts, nxt.Value)
	}
	imp.Path = strings.Join(parts, ".")
	if _, err := c.expect(TokenSemicolon, ";"); err != nil {
		return ImportDecl{}, err
	}
	return imp, nil
}

// readDottedName 读 `a.b.c.D` 形式的 dotted name(包名 / qualified type 名)。
// 接受普通 ident 与 contextual keyword(codex review #2)。 至少一段。 不接受 wildcard。
func readDottedName(c *cursor) (string, error) {
	first := c.peek()
	if !isIdentLike(first) {
		return "", parseError(tokenPos(first), "expected identifier, got %s %q", first.Kind, first.Value)
	}
	c.consume()
	var b strings.Builder
	b.WriteString(first.Value)
	for c.match(TokenDot) {
		nxt := c.peek()
		if !isIdentLike(nxt) {
			return "", parseError(tokenPos(nxt), "expected identifier after '.', got %s %q", nxt.Kind, nxt.Value)
		}
		c.consume()
		b.WriteByte('.')
		b.WriteString(nxt.Value)
	}
	return b.String(), nil
}

// parseError 统一 parser 错误格式。
func parseError(pos Position, format string, args ...interface{}) error {
	return fmt.Errorf("parse error at %d:%d: %s", pos.Line, pos.Col, fmt.Sprintf(format, args...))
}
