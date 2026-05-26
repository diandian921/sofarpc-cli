# Java Declaration Parser — C.1 Lexer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现一个 minimal pure-Go Java lexer(`internal/javaparser/lexer.go`),tokenize Java 源文件成结构化 token 流。 这是 Issue C 长期方案(把 `internal/schema/schema.go` 里的 regex parser 换成 declaration parser)三 plan 系列的**第一份**,只交付 lexer + 完整单元测试,**不接入** 现有 schema 路径。

**Architecture:**
- 新建独立 package `internal/javaparser`,不依赖 schema 包(单向依赖)
- 只识别 Java declaration 层用得到的 token 种类(identifier、keyword、literal、`{}()[]<>,;.=@?` 等 punctuation),不解析 expression / operator semantics
- 处理 line comment / block comment / javadoc(单独 token kind,因为 javadoc 在 declaration 解析时要拿来做 summary)
- 处理 string / char / numeric literal 边界(只记 raw bytes,不做 unescape,因为 declaration 解析不需要 literal 值)
- 不接入任何现有路径,只产出独立 package + ≥95% line-coverage 单元测试
- C.2 (Parser) 在 C.1 完成后,基于本 lexer 的 token 流写 declaration parser
- C.3 (Adapter + cutover) 替换 schema.go 里的 regex 实现,golden test 全绿后删除旧代码

**Tech Stack:** Go 1.21+,标准库 `strings` / `unicode`,既有 `testing` 框架。 **不引入** 任何外部依赖(包括 tree-sitter / antlr / cgo),保持 pure-Go 单二进制承诺。 Codex 调研过 `antlr4-go` / `participle` / `goyacc` / 现有 Go Java parser 库,结论:没有可信替代,hand-write 是 unique optimal path,但 scope 必须卡在 declaration indexer。

**Why this is a separate plan from C.2 / C.3:** Lexer 是纯输入到 token 流的转换,自己有完整 unit test 即可验收。 接入路径(parser + adapter)需要在 C.1 lexer 实际 API 形态确定后才能精确设计;先把这一份打稳,避免 plan 文档 over-specify 还没落地的接口。

**Scope charter — 这是"Java declaration indexer"不是"Java parser"。** 这条边界 codex review 提议,定死本 plan 及 C.2 / C.3 的 maintenance scope:
- **In scope:** package / import(含 wildcard / static / static wildcard)/ class / interface / enum / record / annotation / method signature(modifiers / type params / return type / params / throws)/ field declaration / type ref(qualified name + generic + array)/ annotation use(识别边界,不解析元素语义)
- **Out of scope:** method body / field initializer / expression / lambda / statement —— 全部用大括号平衡 skip 掉,token 流里仍然存在(可 `TokenOther` 归类),但 parser 不解析
- **Identifier 字符集:** 沿用现有 regex parser 的 ASCII 偏置(`[A-Za-z_$][\w]*`),不支持完整 Java Unicode identifier。 这跟现有 schema.go regex 行为一致,标"compatible with current parser"而不是"Java lexer-compliant"
- 任何要不要加新 grammar 的争论,用"是否帮助识别 declaration"来裁断

---

## File Structure

| 文件 | 操作 | 责任 |
|---|---|---|
| `internal/javaparser/token.go` | Create | `TokenKind` enum、`Token` struct(kind、value、line、col、offset)、`String()` |
| `internal/javaparser/lexer.go` | Create | `Tokenize(src []byte) ([]Token, error)` 主入口;内部 lexer state machine |
| `internal/javaparser/keywords.go` | Create | Java 关键字到 `TokenKind` 的映射表 |
| `internal/javaparser/lexer_test.go` | Create | 单元 test:每类 token、注释、literal、edge case |
| `internal/javaparser/testdata/` | Create | 一个完整 Java facade 文件 + golden token sequence |
| `internal/javaparser/testdata/facade.java` | Create | 真实 facade 风格的 Java 输入 |
| `internal/javaparser/testdata/facade.tokens.json` | Create | golden token 序列(JSON,便于 diff) |

---

## Task 1:Package skeleton + Token 类型 + 空 Tokenize 函数

**Files:**
- Create: `internal/javaparser/token.go`
- Create: `internal/javaparser/lexer.go`
- Create: `internal/javaparser/lexer_test.go`

- [ ] **Step 1: 创建 `token.go`**

```go
// Package javaparser 是一个 Java declaration 层 parser(只解析 package / import /
// class / interface / enum / record / annotation / method signature / field
// declaration),不解析 method body / expression / lambda。 用于替换
// internal/schema 里的 regex parser。
package javaparser

import "fmt"

type TokenKind int

const (
	TokenEOF TokenKind = iota
	TokenError

	// 注释
	TokenLineComment  // // ...
	TokenBlockComment // /* ... */
	TokenJavadoc      // /** ... */

	// 标识符与关键字(关键字独立 kind,见 keywords.go)
	TokenIdent
	TokenKeyword

	// 字面量(raw bytes,未 unescape)
	TokenIntLiteral
	TokenLongLiteral
	TokenFloatLiteral
	TokenDoubleLiteral
	TokenStringLiteral
	TokenCharLiteral
	TokenBoolLiteral // true / false
	TokenNullLiteral // null

	// 标点 / 操作符(只列 declaration 用到的)
	TokenLBrace    // {
	TokenRBrace    // }
	TokenLParen    // (
	TokenRParen    // )
	TokenLBracket  // [
	TokenRBracket  // ]
	TokenLAngle    // <
	TokenRAngle    // >
	TokenComma     // ,
	TokenSemicolon // ;
	TokenDot       // .
	TokenAt        // @
	TokenAssign    // =
	TokenQuestion  // ?
	TokenStar      // * (用于 import wildcard)
	TokenColon     // :
	TokenAmp       // & (用于 type bound: T extends A & B)
	TokenEllipsis  // ... (varargs)
	TokenArrow     // -> (lambda;只在 skip body 时遇到)

	// declaration 不关心但要 skip 的 operator/punct 占位
	TokenOther
)

// Token 一个词法单元。
type Token struct {
	Kind  TokenKind
	Value string // raw source bytes
	Line  int    // 1-based
	Col   int    // 1-based,Tab 算 1 列
	Off   int    // byte offset into source
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q) @%d:%d", t.Kind, t.Value, t.Line, t.Col)
}

func (k TokenKind) String() string {
	switch k {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "ERROR"
	case TokenLineComment:
		return "LineComment"
	case TokenBlockComment:
		return "BlockComment"
	case TokenJavadoc:
		return "Javadoc"
	case TokenIdent:
		return "Ident"
	case TokenKeyword:
		return "Keyword"
	case TokenIntLiteral:
		return "Int"
	case TokenLongLiteral:
		return "Long"
	case TokenFloatLiteral:
		return "Float"
	case TokenDoubleLiteral:
		return "Double"
	case TokenStringLiteral:
		return "String"
	case TokenCharLiteral:
		return "Char"
	case TokenBoolLiteral:
		return "Bool"
	case TokenNullLiteral:
		return "Null"
	case TokenLBrace:
		return "{"
	case TokenRBrace:
		return "}"
	case TokenLParen:
		return "("
	case TokenRParen:
		return ")"
	case TokenLBracket:
		return "["
	case TokenRBracket:
		return "]"
	case TokenLAngle:
		return "<"
	case TokenRAngle:
		return ">"
	case TokenComma:
		return ","
	case TokenSemicolon:
		return ";"
	case TokenDot:
		return "."
	case TokenAt:
		return "@"
	case TokenAssign:
		return "="
	case TokenQuestion:
		return "?"
	case TokenStar:
		return "*"
	case TokenColon:
		return ":"
	case TokenAmp:
		return "&"
	case TokenEllipsis:
		return "..."
	case TokenArrow:
		return "->"
	case TokenOther:
		return "Other"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}
```

- [ ] **Step 2: 创建空 `lexer.go`**

```go
package javaparser

// Tokenize 把 Java 源代码切成 token 流。
// 输入是任意 UTF-8 bytes,输出以一个 TokenEOF 结尾。
// 真正会触发 error 的只有未闭合的 comment / string / char / text block;
// 其他无法归类的字符兜底成 TokenOther(允许 declaration parser 用大括号
// 平衡 skip method body 时正确穿越未知 operator)。
// 注意:Task 1 阶段先返回空 stub,Task 2+ 逐步替换实现。
func Tokenize(src []byte) ([]Token, error) {
	return []Token{{Kind: TokenEOF, Line: 1, Col: 1}}, nil
}
```

- [ ] **Step 3: 创建 `lexer_test.go` 烟雾测试**

```go
package javaparser

import "testing"

func TestTokenizeEmptyReturnsEOF(t *testing.T) {
	tokens, err := Tokenize([]byte(""))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(tokens) != 1 || tokens[0].Kind != TokenEOF {
		t.Fatalf("tokens = %#v", tokens)
	}
}
```

- [ ] **Step 4: 跑测试,确认通过**

Run: `go test ./internal/javaparser/ -v`

Expected: PASS。

---

## Task 2:Whitespace + 三种注释

**Files:**
- Modify: `internal/javaparser/lexer.go`(全文重写,从骨架升级到 state machine)
- Modify: `internal/javaparser/lexer_test.go`(加 comment test)

- [ ] **Step 1: 实现 lexer state machine 主循环 + whitespace skip + 三种注释识别**

把 `lexer.go` 替换为(Task 4 才用到 `strings`,这里**不要** import 否则 Go 会 build fail with unused import):

```go
package javaparser

import "fmt"

type lexer struct {
	src  []byte
	pos  int
	line int
	col  int
}

func Tokenize(src []byte) ([]Token, error) {
	l := &lexer{src: src, pos: 0, line: 1, col: 1}
	var out []Token
	for {
		l.skipWhitespace()
		if l.pos >= len(l.src) {
			out = append(out, Token{Kind: TokenEOF, Line: l.line, Col: l.col, Off: l.pos})
			return out, nil
		}
		tok, err := l.next()
		if err != nil {
			out = append(out, tok)
			return out, err
		}
		out = append(out, tok)
	}
}

func (l *lexer) skipWhitespace() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case ' ', '\t', '\r':
			l.advance()
		case '\n':
			l.pos++
			l.line++
			l.col = 1
		default:
			return
		}
	}
}

func (l *lexer) advance() {
	if l.pos < len(l.src) {
		l.pos++
		l.col++
	}
}

func (l *lexer) peek(offset int) byte {
	if l.pos+offset >= len(l.src) {
		return 0
	}
	return l.src[l.pos+offset]
}

func (l *lexer) next() (Token, error) {
	startLine, startCol, startOff := l.line, l.col, l.pos
	c := l.src[l.pos]

	// 三种注释
	if c == '/' && l.peek(1) == '/' {
		return l.readLineComment(startLine, startCol, startOff), nil
	}
	if c == '/' && l.peek(1) == '*' {
		return l.readBlockOrJavadoc(startLine, startCol, startOff)
	}

	// 占位:剩余字符暂时报 error,Task 3+ 替换
	return Token{Kind: TokenError, Value: string(c), Line: startLine, Col: startCol, Off: startOff},
		fmt.Errorf("unrecognized character %q at %d:%d", c, startLine, startCol)
}

func (l *lexer) readLineComment(line, col, off int) Token {
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.advance()
	}
	return Token{Kind: TokenLineComment, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off}
}

func (l *lexer) readBlockOrJavadoc(line, col, off int) (Token, error) {
	start := l.pos
	isJavadoc := l.peek(2) == '*' && l.peek(3) != '/'
	l.advance() // /
	l.advance() // *
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.peek(1) == '/' {
			l.advance() // *
			l.advance() // /
			kind := TokenBlockComment
			if isJavadoc {
				kind = TokenJavadoc
			}
			return Token{Kind: kind, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off}, nil
		}
		if l.src[l.pos] == '\n' {
			l.pos++
			l.line++
			l.col = 1
		} else {
			l.advance()
		}
	}
	return Token{Kind: TokenError, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off},
		fmt.Errorf("unterminated block comment starting at %d:%d", line, col)
}

```

- [ ] **Step 2: 在 `lexer_test.go` 追加注释测试**

```go
func TestTokenizeComments(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []TokenKind
	}{
		{"line", "// hello\n", []TokenKind{TokenLineComment, TokenEOF}},
		{"line_no_newline", "// trailing", []TokenKind{TokenLineComment, TokenEOF}},
		{"block", "/* foo */", []TokenKind{TokenBlockComment, TokenEOF}},
		{"block_multiline", "/* foo\nbar */", []TokenKind{TokenBlockComment, TokenEOF}},
		{"javadoc", "/** doc */", []TokenKind{TokenJavadoc, TokenEOF}},
		{"javadoc_multiline", "/**\n * line\n */", []TokenKind{TokenJavadoc, TokenEOF}},
		{"empty_block", "/**/", []TokenKind{TokenBlockComment, TokenEOF}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := Tokenize([]byte(tc.src))
			if err != nil {
				t.Fatalf("err = %v; tokens = %v", err, tokens)
			}
			got := make([]TokenKind, len(tokens))
			for i, tok := range tokens {
				got[i] = tok.Kind
			}
			if len(got) != len(tc.want) {
				t.Fatalf("kinds = %v, want %v", got, tc.want)
			}
			for i, k := range got {
				if k != tc.want[i] {
					t.Fatalf("kinds[%d] = %v, want %v", i, k, tc.want[i])
				}
			}
		})
	}
}

func TestTokenizeUnterminatedBlockComment(t *testing.T) {
	_, err := Tokenize([]byte("/* unterminated"))
	if err == nil {
		t.Fatal("expected error for unterminated block comment")
	}
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v`

Expected: comment tests 全 PASS;`TestTokenizeEmptyReturnsEOF` 仍 PASS。

---

## Task 3:Identifier + Java 关键字 + bool/null literal

**Files:**
- Create: `internal/javaparser/keywords.go`
- Modify: `internal/javaparser/lexer.go`(在 `next()` 加 identifier 分支)
- Modify: `internal/javaparser/lexer_test.go`

- [ ] **Step 1: 创建 `keywords.go`**

```go
package javaparser

// javaKeywords 是 Java 21 之前的全部保留关键字。
// 注意:'true' / 'false' / 'null' 在 Java 中是 reserved literal,不是 keyword,
// 单独返回 TokenBoolLiteral / TokenNullLiteral。
var javaKeywords = map[string]bool{
	"abstract": true, "assert": true, "boolean": true, "break": true,
	"byte": true, "case": true, "catch": true, "char": true,
	"class": true, "const": true, "continue": true, "default": true,
	"do": true, "double": true, "else": true, "enum": true,
	"extends": true, "final": true, "finally": true, "float": true,
	"for": true, "goto": true, "if": true, "implements": true,
	"import": true, "instanceof": true, "int": true, "interface": true,
	"long": true, "native": true, "new": true, "package": true,
	"private": true, "protected": true, "public": true, "return": true,
	"short": true, "static": true, "strictfp": true, "super": true,
	"switch": true, "synchronized": true, "this": true, "throw": true,
	"throws": true, "transient": true, "try": true, "void": true,
	"volatile": true, "while": true,
	// Java 9+ 上下文关键字,词法层一律识别为 keyword
	"module": true, "open": true, "opens": true, "uses": true,
	"provides": true, "requires": true, "exports": true, "to": true,
	"with": true, "transitive": true,
	// Java 14+ record / sealed / non-sealed / permits 是上下文关键字
	// 词法层都识别为 keyword,parser 按位置消歧
	"record": true, "sealed": true, "permits": true, "yield": true,
	"var": true,
}

// 注意 "non-sealed" 包含连字符,lexer 不识别,parser 层组合 token 时特判。
```

- [ ] **Step 2: 在 `lexer.go` 的 `next()` 函数中,在注释分支后追加 identifier 分支**

把 `next()` 函数体替换为:

```go
func (l *lexer) next() (Token, error) {
	startLine, startCol, startOff := l.line, l.col, l.pos
	c := l.src[l.pos]

	if c == '/' && l.peek(1) == '/' {
		return l.readLineComment(startLine, startCol, startOff), nil
	}
	if c == '/' && l.peek(1) == '*' {
		return l.readBlockOrJavadoc(startLine, startCol, startOff)
	}

	if isIdentStart(c) {
		return l.readIdent(startLine, startCol, startOff), nil
	}

	return Token{Kind: TokenError, Value: string(c), Line: startLine, Col: startCol, Off: startOff},
		fmt.Errorf("unrecognized character %q at %d:%d", c, startLine, startCol)
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func (l *lexer) readIdent(line, col, off int) Token {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.advance()
	}
	value := string(l.src[start:l.pos])
	kind := TokenIdent
	switch value {
	case "true", "false":
		kind = TokenBoolLiteral
	case "null":
		kind = TokenNullLiteral
	default:
		if javaKeywords[value] {
			kind = TokenKeyword
		}
	}
	return Token{Kind: kind, Value: value, Line: line, Col: col, Off: off}
}
```

这一步不需要新 import。

- [ ] **Step 3: 加 identifier 测试**

```go
func TestTokenizeIdentifierAndKeyword(t *testing.T) {
	cases := []struct {
		src  string
		kind TokenKind
		val  string
	}{
		{"foo", TokenIdent, "foo"},
		{"Foo123", TokenIdent, "Foo123"},
		{"_under_score", TokenIdent, "_under_score"},
		{"$dollar", TokenIdent, "$dollar"},
		{"public", TokenKeyword, "public"},
		{"interface", TokenKeyword, "interface"},
		{"record", TokenKeyword, "record"},
		{"true", TokenBoolLiteral, "true"},
		{"false", TokenBoolLiteral, "false"},
		{"null", TokenNullLiteral, "null"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, err := Tokenize([]byte(tc.src))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(tokens) != 2 {
				t.Fatalf("tokens = %v", tokens)
			}
			if tokens[0].Kind != tc.kind || tokens[0].Value != tc.val {
				t.Errorf("tokens[0] = %v, want kind=%v val=%q", tokens[0], tc.kind, tc.val)
			}
		})
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/javaparser/ -v`

Expected: 全 PASS。

---

## Task 4:Numeric / string / char literal

**Files:**
- Modify: `internal/javaparser/lexer.go`
- Modify: `internal/javaparser/lexer_test.go`

- [ ] **Step 1: 在 `next()` 中追加 literal 分支**

`next()` 替换为(增加 numeric / string / char 分支):

```go
func (l *lexer) next() (Token, error) {
	startLine, startCol, startOff := l.line, l.col, l.pos
	c := l.src[l.pos]

	if c == '/' && l.peek(1) == '/' {
		return l.readLineComment(startLine, startCol, startOff), nil
	}
	if c == '/' && l.peek(1) == '*' {
		return l.readBlockOrJavadoc(startLine, startCol, startOff)
	}

	if isIdentStart(c) {
		return l.readIdent(startLine, startCol, startOff), nil
	}

	if c >= '0' && c <= '9' {
		return l.readNumber(startLine, startCol, startOff), nil
	}

	if c == '"' {
		// Java 15+ text block """..."""
		if l.peek(1) == '"' && l.peek(2) == '"' {
			return l.readTextBlock(startLine, startCol, startOff)
		}
		return l.readString(startLine, startCol, startOff)
	}
	if c == '\'' {
		return l.readChar(startLine, startCol, startOff)
	}

	return Token{Kind: TokenError, Value: string(c), Line: startLine, Col: startCol, Off: startOff},
		fmt.Errorf("unrecognized character %q at %d:%d", c, startLine, startCol)
}
```

- [ ] **Step 2: 在 `lexer.go` 末尾追加 `readNumber` / `readString` / `readChar`**

```go
// readNumber 识别 int / long / float / double 字面量。
// 支持 decimal / hex (0x) / binary (0b) / octal (0...),后缀 L/l/F/f/D/d。
// 接收 Java 7+ 下划线分隔(如 1_000_000)。
// 关键:'+'/'-' 必须紧跟 exponent 标志(e/E/p/P)才属于 number 的一部分,
// 否则会把 "1-2" 错误吃成一个 token。
func (l *lexer) readNumber(line, col, off int) Token {
	start := l.pos
	l.advance() // 首字符 0-9 已校验
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		// '+'/'-' 只在指数符号紧后合法(1e+10 / 1E-5 / 0x1.0p+8)。
		if c == '+' || c == '-' {
			if l.pos > 0 {
				prev := l.src[l.pos-1]
				if prev == 'e' || prev == 'E' || prev == 'p' || prev == 'P' {
					l.advance()
					continue
				}
			}
			break
		}
		if (c >= '0' && c <= '9') || c == '_' || c == '.' ||
			(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
			c == 'x' || c == 'X' || c == 'b' || c == 'B' ||
			c == 'e' || c == 'E' || c == 'p' || c == 'P' {
			l.advance()
			continue
		}
		break
	}
	kind := TokenIntLiteral
	if l.pos < len(l.src) {
		switch l.src[l.pos] {
		case 'L', 'l':
			l.advance()
			kind = TokenLongLiteral
		case 'F', 'f':
			l.advance()
			kind = TokenFloatLiteral
		case 'D', 'd':
			l.advance()
			kind = TokenDoubleLiteral
		}
	}
	value := string(l.src[start:l.pos])
	if kind == TokenIntLiteral && (strings.Contains(value, ".") || strings.ContainsAny(value, "eEpP")) {
		kind = TokenDoubleLiteral
	}
	return Token{Kind: kind, Value: value, Line: line, Col: col, Off: off}
}

// readString 识别 "..." 单行字符串字面量(text block 由 readTextBlock 处理)。
// 不解析转义,只识别边界(\" 是转义,不结束字符串)。
// 单行 string 不能跨 newline,遇 newline 返回 error。
func (l *lexer) readString(line, col, off int) (Token, error) {
	start := l.pos
	l.advance() // opening "
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.peek(1) != 0 {
			l.advance()
			l.advance()
			continue
		}
		if c == '"' {
			l.advance()
			return Token{Kind: TokenStringLiteral, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off}, nil
		}
		if c == '\n' {
			return Token{Kind: TokenError, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off},
				fmt.Errorf("unterminated string at %d:%d", line, col)
		}
		l.advance()
	}
	return Token{Kind: TokenError, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off},
		fmt.Errorf("unterminated string at %d:%d", line, col)
}

func (l *lexer) readChar(line, col, off int) (Token, error) {
	start := l.pos
	l.advance() // opening '
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.peek(1) != 0 {
			l.advance()
			l.advance()
			continue
		}
		if c == '\'' {
			l.advance()
			return Token{Kind: TokenCharLiteral, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off}, nil
		}
		if c == '\n' {
			return Token{Kind: TokenError, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off},
				fmt.Errorf("unterminated char at %d:%d", line, col)
		}
		l.advance()
	}
	return Token{Kind: TokenError, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off},
		fmt.Errorf("unterminated char at %d:%d", line, col)
}

// readTextBlock 识别 Java 15+ text block """..."""。
// codex review #13:annotation default 和 interface 常量都可能含 text block,
// 不能只靠"declaration 上下文没有"假设。
// **Lenient 实现**:严格 Java 要求 `"""` 后必须立刻换行,本实现不强制
// 这个语法约束,接受 `"""hello"""` 这种形态。 用途是 declaration 解析时
// 正确穿越 text block 边界(不被 `"` 误伤),不是 Java compiler 级合规。
// 不解析转义,只识别边界,raw value 输出。
func (l *lexer) readTextBlock(line, col, off int) (Token, error) {
	start := l.pos
	l.advance() // "
	l.advance() // "
	l.advance() // "
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' && l.peek(1) == '"' && l.peek(2) == '"' {
			l.advance()
			l.advance()
			l.advance()
			return Token{Kind: TokenStringLiteral, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off}, nil
		}
		if l.src[l.pos] == '\\' && l.peek(1) != 0 {
			l.advance()
			l.advance()
			continue
		}
		if l.src[l.pos] == '\n' {
			l.pos++
			l.line++
			l.col = 1
		} else {
			l.advance()
		}
	}
	return Token{Kind: TokenError, Value: string(l.src[start:l.pos]), Line: line, Col: col, Off: off},
		fmt.Errorf("unterminated text block at %d:%d", line, col)
}
```

`readNumber` 函数体里用到 `strings.Contains` / `strings.ContainsAny`,记得在 `lexer.go` 顶部 import `"strings"`(如果之前 task 移除过)。

- [ ] **Step 3: 加 literal 测试**

```go
func TestTokenizeLiterals(t *testing.T) {
	cases := []struct {
		src  string
		kind TokenKind
		val  string
	}{
		{"42", TokenIntLiteral, "42"},
		{"42L", TokenLongLiteral, "42L"},
		{"3.14", TokenDoubleLiteral, "3.14"},
		{"3.14f", TokenFloatLiteral, "3.14f"},
		{"3.14D", TokenDoubleLiteral, "3.14D"},
		{"1e10", TokenDoubleLiteral, "1e10"},
		{"0xff", TokenIntLiteral, "0xff"},
		{"0xffL", TokenLongLiteral, "0xffL"},
		{"0b1010", TokenIntLiteral, "0b1010"},
		{`"hello"`, TokenStringLiteral, `"hello"`},
		{`"esc\"quote"`, TokenStringLiteral, `"esc\"quote"`},
		{`'a'`, TokenCharLiteral, `'a'`},
		{`'\n'`, TokenCharLiteral, `'\n'`},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, err := Tokenize([]byte(tc.src))
			if err != nil {
				t.Fatalf("err = %v; tokens = %v", err, tokens)
			}
			if len(tokens) != 2 {
				t.Fatalf("tokens = %v", tokens)
			}
			if tokens[0].Kind != tc.kind || tokens[0].Value != tc.val {
				t.Errorf("tokens[0] = %v, want kind=%v val=%q", tokens[0], tc.kind, tc.val)
			}
		})
	}
}

func TestTokenizeUnterminatedString(t *testing.T) {
	_, err := Tokenize([]byte(`"oops`))
	if err == nil {
		t.Fatal("expected unterminated string error")
	}
}

func TestTokenizeUnterminatedChar(t *testing.T) {
	_, err := Tokenize([]byte(`'a`))
	if err == nil {
		t.Fatal("expected unterminated char error")
	}
}

// 指数符号紧后的 '+'/'-' 必须被吃进 number。
// 这个 test 在 Task 4 加完即可跑,不依赖 TokenOther 兜底。
func TestNumberKeepsExponentSign(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1e+10", "1e+10"},
		{"1E-5", "1E-5"},
		{"0x1.0p+8", "0x1.0p+8"},
	}
	for _, tc := range cases {
		tokens, _ := Tokenize([]byte(tc.in))
		if len(tokens) != 2 || tokens[0].Value != tc.want {
			t.Errorf("%q → tokens = %v", tc.in, tokens)
		}
	}
}

// TestNumberDoesNotEatSubtraction:**Task 5 加完 TokenOther 兜底后才能跑**。
// 在 Task 4 阶段,'-' 还是返回 TokenError,这个 test 会 fail。
// codex review #12 的反复重现:之前的实现在任意位置接受 '-',会让 "1-2"
// 被吞成单 token,后续 declaration parser 完全乱掉。
// 把它放在 Task 4 文件里只是为了 colocate number-related test,
// 但**实际跑通在 Task 5**。
func TestNumberDoesNotEatSubtraction(t *testing.T) {
	tokens, err := Tokenize([]byte("1-2"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 期望:Int(1) + Other(-) + Int(2) + EOF
	if len(tokens) != 4 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[0].Kind != TokenIntLiteral || tokens[0].Value != "1" {
		t.Errorf("tokens[0] = %v", tokens[0])
	}
	if tokens[1].Kind != TokenOther || tokens[1].Value != "-" {
		t.Errorf("tokens[1] = %v", tokens[1])
	}
	if tokens[2].Kind != TokenIntLiteral || tokens[2].Value != "2" {
		t.Errorf("tokens[2] = %v", tokens[2])
	}
}

// codex review #13:Java 15+ text block 也可能在 annotation default 或
// interface 常量出现,parser 跳 method body 但解析 field/annotation 时
// 必须能识别 """ 边界。
func TestTokenizeTextBlock(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"single_line", `"""hello"""`},
		{"multiline", "\"\"\"\nfoo\nbar\n\"\"\""},
		{"contains_single_quote", `"""he said "hi" once"""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := Tokenize([]byte(tc.src))
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(tokens) != 2 || tokens[0].Kind != TokenStringLiteral {
				t.Fatalf("tokens = %v", tokens)
			}
			if tokens[0].Value != tc.src {
				t.Errorf("text block raw value = %q, want %q", tokens[0].Value, tc.src)
			}
		})
	}
}

func TestTokenizeUnterminatedTextBlock(t *testing.T) {
	_, err := Tokenize([]byte(`"""oops`))
	if err == nil {
		t.Fatal("expected unterminated text block error")
	}
}
```

- [ ] **Step 4: 跑 Task 4 加完即可通过的 test 子集**

Run: `go test ./internal/javaparser/ -v -skip '^TestNumberDoesNotEatSubtraction$'`

Expected: 全 PASS。 `TestNumberDoesNotEatSubtraction` 跳过 —— 它 expect `TokenOther("-")`,但 `TokenOther` 兜底 Task 5 才加,本步跑会 FAIL。 Task 5 Step 3 解开 skip。

---

## Task 5:Punctuation / operator / annotation marker / `...` / `->`

**Files:**
- Modify: `internal/javaparser/lexer.go`
- Modify: `internal/javaparser/lexer_test.go`

- [ ] **Step 1: 在 `next()` 末尾(error 兜底之前)追加 punctuation 分支**

`next()` 替换为(最终版,完整):

```go
func (l *lexer) next() (Token, error) {
	startLine, startCol, startOff := l.line, l.col, l.pos
	c := l.src[l.pos]

	if c == '/' && l.peek(1) == '/' {
		return l.readLineComment(startLine, startCol, startOff), nil
	}
	if c == '/' && l.peek(1) == '*' {
		return l.readBlockOrJavadoc(startLine, startCol, startOff)
	}

	if isIdentStart(c) {
		return l.readIdent(startLine, startCol, startOff), nil
	}

	if c >= '0' && c <= '9' {
		return l.readNumber(startLine, startCol, startOff), nil
	}

	if c == '"' {
		// Java 15+ text block """..."""
		if l.peek(1) == '"' && l.peek(2) == '"' {
			return l.readTextBlock(startLine, startCol, startOff)
		}
		return l.readString(startLine, startCol, startOff)
	}
	if c == '\'' {
		return l.readChar(startLine, startCol, startOff)
	}

	if c == '.' && l.peek(1) == '.' && l.peek(2) == '.' {
		l.advance()
		l.advance()
		l.advance()
		return Token{Kind: TokenEllipsis, Value: "...", Line: startLine, Col: startCol, Off: startOff}, nil
	}
	if c == '-' && l.peek(1) == '>' {
		l.advance()
		l.advance()
		return Token{Kind: TokenArrow, Value: "->", Line: startLine, Col: startCol, Off: startOff}, nil
	}

	punctMap := map[byte]TokenKind{
		'{': TokenLBrace, '}': TokenRBrace,
		'(': TokenLParen, ')': TokenRParen,
		'[': TokenLBracket, ']': TokenRBracket,
		'<': TokenLAngle, '>': TokenRAngle,
		',': TokenComma, ';': TokenSemicolon,
		'.': TokenDot, '@': TokenAt,
		'=': TokenAssign, '?': TokenQuestion,
		'*': TokenStar, ':': TokenColon, '&': TokenAmp,
	}
	if kind, ok := punctMap[c]; ok {
		l.advance()
		return Token{Kind: kind, Value: string(c), Line: startLine, Col: startCol, Off: startOff}, nil
	}

	// 兜底:其他字符(`!` `|` `~` `+` `-` `%` `^` 等)以 TokenOther 返回。
	// parser 在 skip method body 时遇到不影响 brace 计数,可以全部忽略。
	l.advance()
	return Token{Kind: TokenOther, Value: string(c), Line: startLine, Col: startCol, Off: startOff}, nil
}
```

注意:之前的 error 返回路径全部移除 —— 未知字符走 `TokenOther`,因为 declaration parser 只关心 brace 平衡和 declaration 关键字位置,任何其他字符 skip 即可。 真正会触发 error 的只有 unterminated comment / string / char。

- [ ] **Step 2: 加 punctuation 测试**

```go
func TestTokenizePunctuation(t *testing.T) {
	src := "{}()[]<>,;.@=?*:&"
	want := []TokenKind{
		TokenLBrace, TokenRBrace, TokenLParen, TokenRParen,
		TokenLBracket, TokenRBracket, TokenLAngle, TokenRAngle,
		TokenComma, TokenSemicolon, TokenDot, TokenAt,
		TokenAssign, TokenQuestion, TokenStar, TokenColon, TokenAmp,
		TokenEOF,
	}
	tokens, err := Tokenize([]byte(src))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(tokens) != len(want) {
		t.Fatalf("tokens = %v", tokens)
	}
	for i, tok := range tokens {
		if tok.Kind != want[i] {
			t.Errorf("tokens[%d] = %v, want %v", i, tok.Kind, want[i])
		}
	}
}

func TestTokenizeMultiCharPunct(t *testing.T) {
	cases := []struct {
		src  string
		kind TokenKind
	}{
		{"...", TokenEllipsis},
		{"->", TokenArrow},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, _ := Tokenize([]byte(tc.src))
			if len(tokens) != 2 || tokens[0].Kind != tc.kind {
				t.Errorf("tokens = %v", tokens)
			}
		})
	}
}

func TestTokenizeOtherFallback(t *testing.T) {
	src := "a + b"
	tokens, err := Tokenize([]byte(src))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	wantKinds := []TokenKind{TokenIdent, TokenOther, TokenIdent, TokenEOF}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("tokens = %v", tokens)
	}
	for i, k := range wantKinds {
		if tokens[i].Kind != k {
			t.Errorf("tokens[%d] = %v, want %v", i, tokens[i].Kind, k)
		}
	}
}
```

- [ ] **Step 3: 跑全套 test(包括 Task 4 之前 skip 的)**

Run: `go test ./internal/javaparser/ -v`

Expected: 全 PASS,包括 `TestNumberDoesNotEatSubtraction`(现在 `TokenOther` 兜底已经在,`-` 会归类为 `TokenOther` 而不是 `TokenError`)。

---

## Task 6:位置信息(line / col / offset)正确性

**Files:**
- Modify: `internal/javaparser/lexer_test.go`

- [ ] **Step 1: 加位置测试**

```go
func TestTokenPositions(t *testing.T) {
	src := "package foo;\n  class Bar"
	tokens, err := Tokenize([]byte(src))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []struct {
		kind TokenKind
		val  string
		line int
		col  int
	}{
		{TokenKeyword, "package", 1, 1},
		{TokenIdent, "foo", 1, 9},
		{TokenSemicolon, ";", 1, 12},
		{TokenKeyword, "class", 2, 3},
		{TokenIdent, "Bar", 2, 9},
		{TokenEOF, "", 2, 12},
	}
	if len(tokens) != len(want) {
		t.Fatalf("tokens (%d): %v", len(tokens), tokens)
	}
	for i, w := range want {
		got := tokens[i]
		if got.Kind != w.kind || got.Value != w.val || got.Line != w.line || got.Col != w.col {
			t.Errorf("tokens[%d] = %v; want kind=%v val=%q line=%d col=%d",
				i, got, w.kind, w.val, w.line, w.col)
		}
	}
}
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/javaparser/ -run TestTokenPositions -v`

Expected: PASS。 如果 col 算错(比如 EOF 的 col),说明 `advance` / 换行处理有遗漏 —— 调 `skipWhitespace` 和 `readBlockOrJavadoc` 的换行计数。

---

## Task 7:Real facade golden test

**Files:**
- Create: `internal/javaparser/testdata/facade.java`
- Create: `internal/javaparser/testdata/facade.tokens.json`
- Modify: `internal/javaparser/lexer_test.go`

- [ ] **Step 1: 创建 `testdata/facade.java`**

```java
package com.acme.facade;

import com.acme.dto.QueryRequest;
import com.acme.dto.QueryResponse;
import com.acme.dto.wildcard.*;
import static com.acme.util.Helpers.format;
import java.util.List;
import java.util.Map;

/**
 * 资产查询门面。
 */
public interface AssetFacade {
    /** 查询资产 */
    QueryResponse<List<Asset>> queryAssets(
            @NotNull QueryRequest request,
            Map<String, List<Long>> filters,
            int limit
    );

    /** 兜底 */
    default boolean ping() {
        return true;
    }
}
```

- [ ] **Step 2: 写 env-gated golden 生成 test**

辅助测试,用 `GO_GENERATE=1` env var 触发生成,默认 skip(不污染常规 test run):

```go
func TestGenerateFacadeGolden(t *testing.T) {
	if os.Getenv("GO_GENERATE") != "1" {
		t.Skip("set GO_GENERATE=1 to (re)generate testdata/facade.tokens.json")
	}
	src, err := os.ReadFile("testdata/facade.java")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tokens, err := Tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	var dump []map[string]interface{}
	for _, tok := range tokens {
		dump = append(dump, map[string]interface{}{
			"kind":  tok.Kind.String(),
			"value": tok.Value,
			"line":  tok.Line,
			"col":   tok.Col,
		})
	}
	body, _ := json.MarshalIndent(dump, "", "  ")
	if err := os.WriteFile("testdata/facade.tokens.json", body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
```

需要 import `"encoding/json"` 和 `"os"`。

- [ ] **Step 3: 跑生成器一次**

Run: `GO_GENERATE=1 go test ./internal/javaparser/ -run TestGenerateFacadeGolden -v`

Expected: PASS,`testdata/facade.tokens.json` 生成。

打开 `testdata/facade.tokens.json` **人工审查**:每个 token 的 kind / value 是否符合预期?重点看 4 个 case:
- `*`(wildcard import 的 `.*`)是 `TokenDot` + `TokenStar`
- `@NotNull` 是 `TokenAt` + `TokenIdent(NotNull)`
- javadoc `/** ... */` 是 `TokenJavadoc`(注意:`/**/` 这种 empty block 应该是 `TokenBlockComment`)
- `List<List<Long>>` 是 `TokenIdent(List) + < + TokenIdent(List) + < + TokenIdent(Long) + > + >`(6 个 token,两个 `>` 不合并)

如果任何 token 不对,**不要修 golden**,回头修对应 Task 的 lexer 逻辑,然后重跑 `GO_GENERATE=1`。 审查通过后无需手动改 test —— env gate 已经保证常规 test run 不会重写 golden。

- [ ] **Step 4: 加 golden compare 测试**

```go
func TestTokenizeFacadeMatchesGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/facade.java")
	if err != nil {
		t.Fatalf("read src: %v", err)
	}
	want, err := os.ReadFile("testdata/facade.tokens.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	tokens, err := Tokenize(src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	var dump []map[string]interface{}
	for _, tok := range tokens {
		dump = append(dump, map[string]interface{}{
			"kind":  tok.Kind.String(),
			"value": tok.Value,
			"line":  tok.Line,
			"col":   tok.Col,
		})
	}
	got, _ := json.MarshalIndent(dump, "", "  ")
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("golden mismatch.\nGOT:\n%s\n\nWANT:\n%s\n", got, want)
	}
}
```

import `"bytes"` / `"encoding/json"` / `"os"`。

- [ ] **Step 5: 跑 golden test**

Run: `go test ./internal/javaparser/ -run TestTokenizeFacadeMatchesGolden -v`

Expected: PASS。 如果 fail,说明 lexer 当前行为跟 Step 3 审查时不一致 —— 不要无脑覆盖 golden,先查为什么变了。

---

## Task 8:Coverage 检查 + commit

**Files:** all created

- [ ] **Step 1: 跑 coverage**

Run: `go test ./internal/javaparser/ -cover -v`

Expected: coverage ≥ 95%。 如果某些分支没覆盖到,补 test(常见漏:`readBlockOrJavadoc` 的换行处理、`readNumber` 的 hex/oct/binary 分支、unterminated 各种)。

- [ ] **Step 2: vet + build**

Run: `go vet ./internal/javaparser/ && go build ./internal/javaparser/`

Expected: 无 warning / error。

- [ ] **Step 3: 查看 git status,确认改动只在 internal/javaparser/ 内**

Run: `git status`

Expected:
```
new file:   internal/javaparser/keywords.go
new file:   internal/javaparser/lexer.go
new file:   internal/javaparser/lexer_test.go
new file:   internal/javaparser/token.go
new file:   internal/javaparser/testdata/facade.java
new file:   internal/javaparser/testdata/facade.tokens.json
new file:   docs/plans/2026-05-26-java-declaration-parser-c1-lexer.md
```

不能动 `internal/schema/` 或其他任何包 —— C.1 是 self-contained 工具包,不接入现有路径。

- [ ] **Step 4: 单次 commit**

```bash
git add internal/javaparser/ docs/plans/2026-05-26-java-declaration-parser-c1-lexer.md
git commit -m "$(cat <<'EOF'
feat: 加 pure-Go Java lexer 作为 declaration parser 基础

新增 internal/javaparser 包,提供 Tokenize(src []byte) ([]Token, error)
入口。支持 Java 21 之前的关键字、上下文关键字 (record/sealed/var)、
identifier、numeric / string / char literal、注释(line/block/javadoc)、
declaration 用到的全部标点、annotation marker (@)、varargs (...)、
import wildcard (*)、generic 边界 (< >)、type bound (&)。

不接入任何现有路径,仅作为 Issue C 长期方案(替换 schema 包 regex
parser)三 plan 系列的第一份。C.2 (parser) 与 C.3 (adapter + cutover)
将基于本 lexer API 单独 plan。

Golden test:internal/javaparser/testdata/facade.java 覆盖真实 facade
风格,含 wildcard import / static import / javadoc / 嵌套 generic /
varargs 等关键 case。Coverage ≥ 95%。
EOF
)"
```

Note:per `~/.claude/CLAUDE.md` 全局规则,**不附加 `Co-Authored-By` trailer**。

---

## Verification

完成全部 Task 后:

```bash
go test ./internal/javaparser/ -v       # 全 PASS
go test ./internal/javaparser/ -cover   # coverage ≥ 95%
go vet ./internal/javaparser/           # 无 warning
go build ./...                          # 整项目仍能 build
```

完成标志:
- `internal/javaparser` 包独立可用,有 `Tokenize` 入口
- 全套现有 schema / app / direct 测试无任何变化(说明没接入现有路径,无副作用)
- Golden test `testdata/facade.tokens.json` 覆盖真实 facade 风格

## Out of Scope(留作 follow-up)

- **Unicode escape** `\uXXXX`:Java 源码可以在任意位置写 unicode escape,严格 lexer 要先 normalize 整段 source 再 tokenize。当前实现忽略这层,假设 source 是 UTF-8。
- **Full Java Unicode identifier**:本 plan 沿用 ASCII + `_$` 偏置,与现有 schema.go regex parser 兼容。完整 Unicode identifier 需要换 `unicode.IsLetter` 之类,留到真实业务遇到为止。
- **Non-sealed** 关键字含连字符:lexer 不识别,token 流里是 `non` + `-` (TokenOther) + `sealed`。parser 层在看到 `non` 后特判。

## What Comes Next(C.2 + C.3 预告)

- **C.2 Java Declaration Parser**:基于本 lexer 实现 recursive descent parser,输出 AST:`CompilationUnit / PackageDecl / ImportDecl / TypeDecl / MethodDecl / FieldDecl / TypeRef / Annotation`。 关键设计点(C.2 plan 时再敲定):
  - import decl 的 4 种形态(regular / wildcard / static regular / static wildcard)统一表达
  - generic args 在 TypeRef 节点上保留完整树(`Map<String, List<X>>` → `TypeRef{name:Map, args:[TypeRef{name:String}, TypeRef{name:List, args:[...]}]}`)
  - method body / field initializer / annotation 参数 一律 skip,只计 brace 平衡
  - nested class 递归
- **C.3 Adapter + Cutover**:把 C.2 AST 转成现有 `schema.Method` / `schema.TypeSchema` / `schema.Parameter` / `schema.Field` 结构,替换 `schema.go` 里 `parseJavaFile / parseMethods / parseTypes / parseFields / parseImports` 的实现。保持 `BuildIndex` / `Search` / `Describe` 对外签名不变。 跑现有 golden test 全绿,新增 wildcard import / static import / inner class 三个新 golden case,然后删除旧 regex 代码。
