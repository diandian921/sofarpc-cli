package javaparser

// cursor 对 []Token 的有状态游标。
// 所有 declaration parser 函数都靠它推进 token。
//
// 设计要点:
//   - 注释 token(LineComment / BlockComment / Javadoc)默认 **跳过**,但
//     `peekJavadoc` / `consumeJavadoc` 可显式取最近一段 javadoc 给 preamble 用。
//   - EOF 一定存在(C.1 lexer 保证):pos 越界时 peek 返回 TokenEOF token。
//   - 不做 lookahead 缓存,实测足够快;需要时直接 idx+N peek。
type cursor struct {
	tokens []Token
	idx    int
}

func newCursor(tokens []Token) *cursor {
	return &cursor{tokens: tokens, idx: 0}
}

// skipTrivia 推进 idx 跳过注释 token。 javadoc 也跳 —— preamble 解析时
// 走另一条路径(peekJavadoc 直接看 idx 之前的最后一段)。
func (c *cursor) skipTrivia() {
	for c.idx < len(c.tokens) {
		k := c.tokens[c.idx].Kind
		if k == TokenLineComment || k == TokenBlockComment || k == TokenJavadoc {
			c.idx++
			continue
		}
		return
	}
}

// peek 返回当前 token(自动 skipTrivia)。 EOF 时返回 EOF token。
func (c *cursor) peek() Token {
	c.skipTrivia()
	if c.idx >= len(c.tokens) {
		return Token{Kind: TokenEOF}
	}
	return c.tokens[c.idx]
}

// peekN 返回从当前位置(已 skipTrivia 后)向后第 n 个非 trivia token。
// n=0 等价于 peek。 越界返回 TokenEOF。
func (c *cursor) peekN(n int) Token {
	c.skipTrivia()
	count := 0
	for i := c.idx; i < len(c.tokens); i++ {
		k := c.tokens[i].Kind
		if k == TokenLineComment || k == TokenBlockComment || k == TokenJavadoc {
			continue
		}
		if count == n {
			return c.tokens[i]
		}
		count++
	}
	return Token{Kind: TokenEOF}
}

// consume 推进并返回当前 token。
func (c *cursor) consume() Token {
	c.skipTrivia()
	if c.idx >= len(c.tokens) {
		return Token{Kind: TokenEOF}
	}
	tok := c.tokens[c.idx]
	c.idx++
	return tok
}

// match 若当前 token kind 匹配则消费并返回 true,否则 false 且不推进。
func (c *cursor) match(kind TokenKind) bool {
	if c.peek().Kind != kind {
		return false
	}
	c.consume()
	return true
}

// matchKeyword 当前 token 是 Keyword 且 value 等于 kw 时消费并返回 true。
func (c *cursor) matchKeyword(kw string) bool {
	tok := c.peek()
	if tok.Kind != TokenKeyword || tok.Value != kw {
		return false
	}
	c.consume()
	return true
}

// matchIdentValue 当前 token 是 Ident 且 value 等于 want 时消费并返回 true。
// 用于上下文关键字(record / sealed / permits / yield / var)
// —— 注意 javaKeywords 已经把它们识别为 TokenKeyword,所以**优先用 matchKeyword**;
// matchIdentValue 仅用于 `non` / `sealed` 拼接 non-sealed 这种特殊场景。
func (c *cursor) matchIdentValue(want string) bool {
	tok := c.peek()
	if tok.Kind != TokenIdent || tok.Value != want {
		return false
	}
	c.consume()
	return true
}

// expect 消费 kind 类型 token 并返回。 不匹配则返回 parseError。
func (c *cursor) expect(kind TokenKind, what string) (Token, error) {
	tok := c.peek()
	if tok.Kind != kind {
		return tok, parseError(tokenPos(tok), "expected %s, got %s %q", what, tok.Kind, tok.Value)
	}
	return c.consume(), nil
}

// expectKeyword 消费指定 keyword 并返回。
func (c *cursor) expectKeyword(kw string) (Token, error) {
	tok := c.peek()
	if tok.Kind != TokenKeyword || tok.Value != kw {
		return tok, parseError(tokenPos(tok), "expected keyword %q, got %s %q", kw, tok.Kind, tok.Value)
	}
	return c.consume(), nil
}

// eof 当前是否 EOF。
func (c *cursor) eof() bool {
	return c.peek().Kind == TokenEOF
}

// pos 当前 token 位置(已 skipTrivia)。
func (c *cursor) pos() Position {
	return tokenPos(c.peek())
}

// tokenPos 把 Token 转为 Position。
func tokenPos(t Token) Position {
	return Position{Line: t.Line, Col: t.Col, Off: t.Off}
}

// peekJavadoc 找当前位置之前最后一段 javadoc(不消费),并要求中间没有
// **非 trivia 的** token 间隔(允许 line/block comment,不允许 ident/keyword/punct)。
// 也就是 javadoc 必须直接挂在下一个声明的上方。
// 找不到返回空字符串。
func (c *cursor) peekJavadoc() string {
	for i := c.idx - 1; i >= 0; i-- {
		k := c.tokens[i].Kind
		switch k {
		case TokenJavadoc:
			return c.tokens[i].Value
		case TokenLineComment, TokenBlockComment:
			continue
		default:
			return ""
		}
	}
	return ""
}

// skipBalanced 在当前 token 是 open 时,平衡消费到匹配的 close 为止(含 close)。
// 内部允许嵌套同种 open/close;不识别其他类型 punctuation 的平衡。
// 用于 method body `{}` / annotation args `()` / generic args `<>` 整段 skip 场景。
// 注意 angle bracket 模式因为 < / > 在表达式中也作为比较 operator 会出现错配,
// 所以 **不建议在 method body 内** 用 angle 模式;那时只用 brace 模式即可
// (method body 内任何 `<` `>` 不影响 brace 计数)。
func (c *cursor) skipBalanced(open, close TokenKind) error {
	startPos := c.pos()
	if !c.match(open) {
		tok := c.peek()
		return parseError(startPos, "skipBalanced: expected %s, got %s %q", open, tok.Kind, tok.Value)
	}
	depth := 1
	for depth > 0 {
		if c.eof() {
			return parseError(startPos, "unmatched %s, hit EOF", open)
		}
		tok := c.consume()
		switch tok.Kind {
		case open:
			depth++
		case close:
			depth--
		}
	}
	return nil
}

// skipUntil 推进直到遇到 kind 之一,或 EOF。 不消费目标 token。
// 返回是否找到目标(EOF 时返回 false)。
func (c *cursor) skipUntil(kinds ...TokenKind) bool {
	for !c.eof() {
		k := c.peek().Kind
		for _, want := range kinds {
			if k == want {
				return true
			}
		}
		c.consume()
	}
	return false
}
