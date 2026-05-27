# Java Declaration Parser — C.2 AST Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 基于 C.1 lexer 的 token 流,实现一个 pure-Go Java declaration parser(`internal/javaparser/parser.go` 等),产出 AST 节点(`CompilationUnit / PackageDecl / ImportDecl / TypeDecl / MethodDecl / FieldDecl / TypeRef / Annotation / TypeParam` 等)。 仍然 **不接入** 现有 `internal/schema` 路径,只交付独立 AST + 完整单元测试。 C.3 单独 plan 把 AST 转 `schema.Method / schema.TypeSchema` 替换现有 regex parser。

**Architecture:**
- Recursive descent parser,消费 `Tokenize(src []byte)` 的 token 切片
- AST 顶点:`Parse(src []byte, sourceFile string) (*CompilationUnit, error)`
- TypeRef 节点保留完整泛型树(`Map<String, List<X>>` → 嵌套 TypeRef);C.3 adapter 用 `TypeRef.String()` 转回字符串塞回 `schema.Method.Parameters[i].Type`
- TypeDecl / MethodDecl 保留 **declared type parameters**(`class Page<T, K>` 的 `<T, K>`、generic method 的 `<T>`)—— 根治 [[rpc-types-generic-preservation]] P3 edge case
- Import declaration 4 种形态(regular / wildcard / static regular / static wildcard)统一在 `ImportDecl{Path, Static, Wildcard}` 表达
- Method body / field initializer / static init / annotation argument 一律 brace/paren balanced skip,只看平衡不解析内部
- Annotation 只识别 marker(`Name`),不存 argument(declaration 索引不需要)
- Nested type 递归(inner class / inner enum 等)
- `non-sealed` 三 token 在 modifier 位置 peek 后合并成 modifier `"non-sealed"`
- `>>` `>>>` 不存在(C.1 lexer 不合并),parser 嵌套泛型靠相邻 `TokenRAngle` 自然处理

**Scope charter — Java declaration indexer,不是 Java parser。** 跟 [[c1-lexer]] 一致,这条边界定死本 plan 的 maintenance scope:
- **In scope:** package / import / class / interface / enum / record / @interface / 顶层与嵌套 type / method signature / field declaration / TypeRef 完整泛型 / annotation marker(识别边界)/ declared type parameters / extends / implements / permits / throws
- **Out of scope:** method body / field initializer / annotation argument 语义 / lambda / 表达式 / statement —— 全部用 brace 平衡 skip,token 流仍存在但 parser 不解析
- **Identifier 字符集:** 沿用 C.1 lexer 的 ASCII + `_$` 偏置
- **Modifier list 是 raw token sequence**:parser 只搜集成 `[]string`,不做 mutual exclusion 校验(spec 的事,parser 不挡)
- 任何"要不要加新 grammar"的争论,用"是否帮助识别 declaration"裁断

**Why separate from C.3:** parser AST + 完整单元 test 是 self-contained 工件,golden test 验收即可。 adapter(C.3)涉及现有 `schema.BuildIndex / Search / Describe` 对外接口、所有下游 caller(invoke.go / mcp / cli)、删除旧 regex 实现等大动作,需要先把 AST 形态打稳再设计接入面。

**Tech Stack:** Go 1.21+,标准库 `strings` / `fmt` / `encoding/json`(test 用)/ `os`(test 用),既有 `testing` 框架。 复用 `internal/javaparser/lexer.go` 和 `internal/javaparser/token.go`。 **不引入** 任何外部依赖。

---

## File Structure

| 文件 | 操作 | 责任 |
|---|---|---|
| `internal/javaparser/ast.go` | Create | 全部 AST 节点类型:`CompilationUnit / PackageDecl / ImportDecl / TypeDecl / TypeKind / TypeParam / TypeRef / WildcardKind / Annotation / MethodDecl / ParamDecl / FieldDecl / EnumValue / Position` + `TypeRef.String()` + `TypeKind.String()` |
| `internal/javaparser/cursor.go` | Create | `cursor` 类型(对 `[]Token` 的有状态游标)+ `peek / peekKind / consume / expect / match / matchKeyword / skipBalanced / skipUntil / pos / err` 等工具方法 |
| `internal/javaparser/parser.go` | Create | `Parse(src []byte, sourceFile string) (*CompilationUnit, error)` 主入口;`parseCompilationUnit / parsePackage / parseImport / parsePreamble`(modifiers + annotations + javadoc) |
| `internal/javaparser/typeref.go` | Create | `parseTypeRef / parseTypeArgs / parseTypeParams / parseTypeBound / parseWildcard / readArrayDims / readQualifiedName` |
| `internal/javaparser/decls.go` | Create | `parseTypeDecl / parseClassOrInterfaceBody / parseMember / parseMethodDecl / parseFieldDecl / parseEnumBody / parseRecordHeader / parseAnnotationBody / skipExtendsImplementsPermits / skipFieldInitializer / skipMethodBody / skipAnnotationArgs` |
| `internal/javaparser/parser_test.go` | Create | 全部 unit test |
| `internal/javaparser/testdata/parser/facade_v2.java` | Create | 较完整 facade fixture(含 nested type / generic / wildcard import / record / enum / annotation marker) |
| `internal/javaparser/testdata/parser/facade_v2.ast.json` | Create | Golden AST 序列化,`GO_GENERATE=1 go test` 重生成 |

**不动**:`lexer.go` / `token.go` / `keywords.go` / `testdata/facade.java` / `testdata/facade.tokens.json`(C.1 工件),`internal/schema/`(C.3 之前不动),`internal/app/`(不接入)。

---

## Task 1:AST 类型 + 空 Parse 入口 + 烟雾测试

**Files:**
- Create: `internal/javaparser/ast.go`
- Create: `internal/javaparser/parser.go`
- Create: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 创建 `ast.go`**

```go
package javaparser

import (
	"fmt"
	"strings"
)

// Position 源文件位置,1-based。 Off 是 byte offset。
type Position struct {
	Line int
	Col  int
	Off  int
}

// CompilationUnit 单个 .java 文件解析结果。
type CompilationUnit struct {
	SourceFile string
	Package    *PackageDecl  // 可空(default package)
	Imports    []ImportDecl
	Types      []TypeDecl
}

// PackageDecl `package a.b.c;`
type PackageDecl struct {
	Name string // dotted FQN
	Pos  Position
}

// ImportDecl 4 种形态统一表达:
//
//	import a.b.C;            → {Path:"a.b.C", Static:false, Wildcard:false}
//	import a.b.*;            → {Path:"a.b",   Static:false, Wildcard:true}
//	import static a.b.C.foo; → {Path:"a.b.C.foo", Static:true,  Wildcard:false}
//	import static a.b.C.*;   → {Path:"a.b.C", Static:true,  Wildcard:true}
//
// Path 永远不含尾部 ".*";Wildcard=true 即表示 wildcard 形态。
type ImportDecl struct {
	Path     string
	Static   bool
	Wildcard bool
	Pos      Position
}

// TypeKind 区分顶层与嵌套 type 的 5 种形态。
type TypeKind int

const (
	TypeKindClass TypeKind = iota
	TypeKindInterface
	TypeKindEnum
	TypeKindRecord
	TypeKindAnnotation // @interface
)

func (k TypeKind) String() string {
	switch k {
	case TypeKindClass:
		return "class"
	case TypeKindInterface:
		return "interface"
	case TypeKindEnum:
		return "enum"
	case TypeKindRecord:
		return "record"
	case TypeKindAnnotation:
		return "annotation"
	}
	return fmt.Sprintf("TypeKind(%d)", int(k))
}

// TypeDecl 一个 type declaration(class / interface / enum / record / @interface)。
// 顶层与嵌套通用。 NestedTypes 递归装载内部 type。
type TypeDecl struct {
	Kind        TypeKind
	Modifiers   []string  // public / abstract / final / sealed / non-sealed / static / ...
	Annotations []Annotation
	Javadoc     string    // raw javadoc body(去除 /** */ + 行首 * 后的纯文本)
	Name        string
	TypeParams  []TypeParam // class Page<T, K extends Y> 的 <T, K extends Y>
	Extends     []TypeRef   // class: 至多 1 个;interface: 多个
	Implements  []TypeRef
	Permits     []TypeRef   // sealed types

	// Body 内容(按 kind 取舍)
	Methods          []MethodDecl
	Fields           []FieldDecl
	EnumValues       []EnumValue   // 仅 enum
	RecordComponents []ParamDecl   // 仅 record header
	NestedTypes      []TypeDecl

	Pos Position
}

// TypeParam declared type parameter:`T extends A & B`。
type TypeParam struct {
	Name   string
	Bounds []TypeRef // 0+ 个;`T extends A & B` → 2 个
}

// WildcardKind 区分 `?` / `? extends X` / `? super X`。
type WildcardKind int

const (
	WildcardNone WildcardKind = iota
	WildcardUnbounded
	WildcardExtends
	WildcardSuper
)

// TypeRef 一个类型引用(出现在 method return / parameter / field type / extends / implements / throws / type bound / generic arg 位置)。
//
// 形态拆分:
//
//   - 普通命名类型:Name="java.util.List", Args=[TypeRef{Name:"X"}], ArrayDims=0
//   - 泛型嵌套:Name="Map", Args=[TypeRef{Name:"String"}, TypeRef{Name:"List", Args:[...]}]
//   - Wildcard:IsWildcard=true,WildcardKind 与 WildcardBound 决定形态
//   - Array:ArrayDims=N(`String[][]` → 2);varargs 也走 ArrayDims=1,由 ParamDecl.IsVarargs 标记
//   - Primitive:Name="int" / "long" / "boolean" / ... (Args 空)
//
// **不做** import 解析:Name 是源码原样(可以是 simple name "List" 也可以是 qualified
// "java.util.List")。 resolveBaseType 是 C.3 adapter 的事。
type TypeRef struct {
	Name          string
	Args          []TypeRef
	ArrayDims     int
	IsWildcard    bool
	WildcardKind  WildcardKind
	WildcardBound *TypeRef // WildcardKind==Extends / Super 时非空
	Pos           Position
}

// String 把 TypeRef 序列化回 Java 源码形态字符串。
// 用于 C.3 adapter 灌回 schema.Method.Parameters[i].Type / schema.Field.Type。
// 不解析 import,输出 Name 原样;array suffix 拼在最外层;wildcard 形态原样输出。
func (t TypeRef) String() string {
	if t.IsWildcard {
		var b strings.Builder
		b.WriteString("?")
		switch t.WildcardKind {
		case WildcardExtends:
			b.WriteString(" extends ")
			if t.WildcardBound != nil {
				b.WriteString(t.WildcardBound.String())
			}
		case WildcardSuper:
			b.WriteString(" super ")
			if t.WildcardBound != nil {
				b.WriteString(t.WildcardBound.String())
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(t.Name)
	if len(t.Args) > 0 {
		b.WriteString("<")
		for i, a := range t.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(a.String())
		}
		b.WriteString(">")
	}
	for i := 0; i < t.ArrayDims; i++ {
		b.WriteString("[]")
	}
	return b.String()
}

// Annotation 只记 marker 名(simple 或 qualified),不存 argument。
// 比如 `@RequestMapping(value = "/x")` → {Name:"RequestMapping"}。
type Annotation struct {
	Name string
	Pos  Position
}

// MethodDecl 方法 / 构造器声明。
// Body 已 skip,parser 不持有 body token。
type MethodDecl struct {
	Modifiers     []string
	Annotations   []Annotation
	Javadoc       string
	TypeParams    []TypeParam // generic method `<T> T foo(...)`
	ReturnType    TypeRef     // ctor 时 ReturnType.Name == 空字符串
	Name          string
	Params        []ParamDecl
	Throws        []TypeRef
	IsConstructor bool
	Pos           Position
}

// ParamDecl 一个方法参数 / record component。
type ParamDecl struct {
	Annotations []Annotation
	Final       bool
	Type        TypeRef
	Name        string
	IsVarargs   bool
}

// FieldDecl 一个字段声明(已 skip initializer)。
// 注意:`int a, b, c;` 这种 multi-decl 会展开成 3 个 FieldDecl(同 Modifiers/Annotations/Javadoc/Type,
// 不同 Name)。
type FieldDecl struct {
	Modifiers   []string
	Annotations []Annotation
	Javadoc     string
	Type        TypeRef
	Name        string
	Pos         Position
}

// EnumValue 一个 enum 常量。 C.2 不解析 enum constant arguments —— `RED(1)` 的 `(1)`
// 在 parseEnumBody 里 brace-balanced skip 掉,不存进 AST。
type EnumValue struct {
	Annotations []Annotation
	Javadoc     string
	Name        string
	Pos         Position
}
```

- [ ] **Step 2: 创建空 `parser.go` stub**

```go
package javaparser

import "fmt"

// Parse 把 Java 源代码解析为 CompilationUnit AST。
// 输入是任意 UTF-8 bytes + 源文件路径(用于 AST 中 SourceFile 字段)。
// 返回的 AST 不做 import 解析、不做 type FQN 解析 —— 那是 C.3 adapter 的事。
//
// 错误分类:
//   - lexer 错误(unterminated comment/string/char/text block)直接透传
//   - parser 错误统一格式 "parse error at L:C: <reason>"
//
// 注意:Task 1 阶段只返回空 stub,Task 2+ 逐步替换实现。
func Parse(src []byte, sourceFile string) (*CompilationUnit, error) {
	tokens, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	_ = tokens
	return &CompilationUnit{SourceFile: sourceFile}, nil
}

// parseError 统一 parser 错误格式。
func parseError(pos Position, format string, args ...interface{}) error {
	return fmt.Errorf("parse error at %d:%d: %s", pos.Line, pos.Col, fmt.Sprintf(format, args...))
}
```

- [ ] **Step 3: 创建 `parser_test.go` 烟雾测试**

```go
package javaparser

import "testing"

func TestParseEmptyReturnsEmptyCompilationUnit(t *testing.T) {
	cu, err := Parse([]byte(""), "Empty.java")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cu == nil {
		t.Fatal("cu = nil")
	}
	if cu.SourceFile != "Empty.java" {
		t.Errorf("SourceFile = %q, want Empty.java", cu.SourceFile)
	}
	if cu.Package != nil {
		t.Errorf("Package = %#v, want nil", cu.Package)
	}
	if len(cu.Imports) != 0 || len(cu.Types) != 0 {
		t.Errorf("Imports/Types non-empty: %#v / %#v", cu.Imports, cu.Types)
	}
}

func TestTypeRefString(t *testing.T) {
	cases := []struct {
		in   TypeRef
		want string
	}{
		{TypeRef{Name: "String"}, "String"},
		{TypeRef{Name: "int"}, "int"},
		{TypeRef{Name: "String", ArrayDims: 2}, "String[][]"},
		{TypeRef{Name: "List", Args: []TypeRef{{Name: "X"}}}, "List<X>"},
		{
			TypeRef{Name: "Map", Args: []TypeRef{{Name: "String"}, {Name: "List", Args: []TypeRef{{Name: "Y"}}}}},
			"Map<String, List<Y>>",
		},
		{TypeRef{IsWildcard: true, WildcardKind: WildcardUnbounded}, "?"},
		{TypeRef{IsWildcard: true, WildcardKind: WildcardExtends, WildcardBound: &TypeRef{Name: "Number"}}, "? extends Number"},
		{TypeRef{IsWildcard: true, WildcardKind: WildcardSuper, WildcardBound: &TypeRef{Name: "Integer"}}, "? super Integer"},
		{
			TypeRef{Name: "List", Args: []TypeRef{{IsWildcard: true, WildcardKind: WildcardExtends, WildcardBound: &TypeRef{Name: "Number"}}}},
			"List<? extends Number>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := tc.in.String()
			if got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/javaparser/ -v`

Expected: 全 PASS(C.1 lexer 既有 test 不受影响)。

- [ ] **Step 5: commit**

```bash
git add internal/javaparser/ast.go internal/javaparser/parser.go internal/javaparser/parser_test.go
git commit -m "feat: 加 javaparser AST 节点定义与 Parse 入口 stub"
```

---

## Task 2:Token cursor 工具(cursor.go)+ unit test

**Files:**
- Create: `internal/javaparser/cursor.go`
- Modify: `internal/javaparser/parser_test.go`(末尾追加)

- [ ] **Step 1: 创建 `cursor.go`**

```go
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
```

- [ ] **Step 2: 在 `parser_test.go` 末尾追加 cursor 测试**

```go
func TestCursorPeekConsumeSkipsTrivia(t *testing.T) {
	tokens, err := Tokenize([]byte("/** doc */ public class Foo { }"))
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	c := newCursor(tokens)

	first := c.peek()
	if first.Kind != TokenKeyword || first.Value != "public" {
		t.Fatalf("peek[0] = %v, want public", first)
	}
	c.consume()
	second := c.consume()
	if second.Kind != TokenKeyword || second.Value != "class" {
		t.Fatalf("consume[1] = %v, want class", second)
	}
}

func TestCursorPeekJavadoc(t *testing.T) {
	// peekJavadoc 设计:从 idx-1 向前找最近的 Javadoc,中间只能是 trivia。
	// 因此必须在 **消费 public 之前** 调用 —— 此时 idx 已被 skipTrivia 推到 public,
	// idx-1 是 Javadoc。 若先 consume(public),idx 推到 class,回看遇到 public(非 trivia)
	// 立即返回 ""。 见 subagent T2 execution flag(2026-05-27)。
	tokens, err := Tokenize([]byte("/** hello */ public class Foo {}"))
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	c := newCursor(tokens)
	c.peek() // 触发 skipTrivia 把 idx 推到 public 上;不消费
	doc := c.peekJavadoc()
	if doc == "" || !contains(doc, "hello") {
		t.Errorf("javadoc = %q, want contains 'hello'", doc)
	}
}

func TestCursorPeekJavadocBlockedByOtherTokens(t *testing.T) {
	tokens, err := Tokenize([]byte("/** doc */ private int x; public void m() {}"))
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	c := newCursor(tokens)
	// 把游标推到 public 之前
	for {
		tok := c.peek()
		if tok.Kind == TokenKeyword && tok.Value == "public" {
			break
		}
		c.consume()
	}
	doc := c.peekJavadoc()
	if doc != "" {
		t.Errorf("javadoc = %q, want empty (blocked by intervening declaration)", doc)
	}
}

func TestCursorMatchAndExpect(t *testing.T) {
	tokens, _ := Tokenize([]byte("{ } ( )"))
	c := newCursor(tokens)
	if !c.match(TokenLBrace) {
		t.Fatal("match LBrace failed")
	}
	if _, err := c.expect(TokenRBrace, "}"); err != nil {
		t.Fatalf("expect RBrace: %v", err)
	}
	if _, err := c.expect(TokenLParen, "("); err != nil {
		t.Fatalf("expect LParen: %v", err)
	}
	if c.match(TokenLBrace) {
		t.Fatal("match LBrace unexpectedly succeeded")
	}
}

func TestCursorSkipBalancedNested(t *testing.T) {
	tokens, _ := Tokenize([]byte("{ a { b } c { d { e } } } trailing"))
	c := newCursor(tokens)
	if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
		t.Fatalf("skipBalanced: %v", err)
	}
	tok := c.peek()
	if tok.Kind != TokenIdent || tok.Value != "trailing" {
		t.Errorf("after skip: %v, want Ident(trailing)", tok)
	}
}

func TestCursorSkipBalancedUnmatched(t *testing.T) {
	tokens, _ := Tokenize([]byte("{ no close"))
	c := newCursor(tokens)
	if err := c.skipBalanced(TokenLBrace, TokenRBrace); err == nil {
		t.Fatal("expected unmatched error")
	}
}

func TestCursorSkipUntilFound(t *testing.T) {
	tokens, _ := Tokenize([]byte("a b ; c"))
	c := newCursor(tokens)
	if !c.skipUntil(TokenSemicolon) {
		t.Fatal("skipUntil semicolon not found")
	}
	if c.peek().Kind != TokenSemicolon {
		t.Errorf("after skipUntil: %v, want ;", c.peek())
	}
}

func TestCursorPeekN(t *testing.T) {
	tokens, _ := Tokenize([]byte("/** d */ a b /* c */ c d"))
	c := newCursor(tokens)
	if v := c.peekN(0); v.Value != "a" {
		t.Errorf("peekN(0) = %v, want a", v)
	}
	if v := c.peekN(1); v.Value != "b" {
		t.Errorf("peekN(1) = %v, want b", v)
	}
	if v := c.peekN(2); v.Value != "c" {
		t.Errorf("peekN(2) = %v, want c (block comment skipped)", v)
	}
}

// contains 是 strings.Contains 的简写,test only。
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v -run TestCursor`

Expected: 全 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/javaparser/cursor.go internal/javaparser/parser_test.go
git commit -m "feat: 加 javaparser token cursor 工具"
```

---

## Task 3:CompilationUnit:package + 4 种 import 形态

**Files:**
- Modify: `internal/javaparser/parser.go`(替换 `Parse` 实现 + 加 `parseCompilationUnit / parsePackage / parseImport / readDottedName`)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 替换 `parser.go`**

```go
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
		// Task 5+ 在这里接入 parseTypeDecl;Task 3 阶段把剩余内容 skip 一格,
		// 避免无限循环。 type decl annotation(@Foo public class X)会落到这条路径,
		// Task 3 测试不会走到那(用例只有 package + imports)。
		c.consume()
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
```

- [ ] **Step 2: 加 package + import 测试**

```go
func TestParsePackageOnly(t *testing.T) {
	cu, err := Parse([]byte("package com.acme.facade;"), "Foo.java")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cu.Package == nil {
		t.Fatal("Package = nil")
	}
	if cu.Package.Name != "com.acme.facade" {
		t.Errorf("Package.Name = %q, want com.acme.facade", cu.Package.Name)
	}
}

func TestParseImportsAllForms(t *testing.T) {
	src := `package p;
import a.b.C;
import a.b.D;
import a.b.*;
import static a.b.C.foo;
import static a.b.C.*;
`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []ImportDecl{
		{Path: "a.b.C"},
		{Path: "a.b.D"},
		{Path: "a.b", Wildcard: true},
		{Path: "a.b.C.foo", Static: true},
		{Path: "a.b.C", Static: true, Wildcard: true},
	}
	if len(cu.Imports) != len(want) {
		t.Fatalf("imports len = %d, want %d (imports=%+v)", len(cu.Imports), len(want), cu.Imports)
	}
	for i, w := range want {
		got := cu.Imports[i]
		if got.Path != w.Path || got.Static != w.Static || got.Wildcard != w.Wildcard {
			t.Errorf("imports[%d] = %+v, want %+v (path/static/wildcard only)", i, got, w)
		}
	}
}

func TestParsePackageWithFileLevelAnnotation(t *testing.T) {
	src := "@Deprecated @SuppressWarnings(\"all\") package p;"
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cu.Package == nil || cu.Package.Name != "p" {
		t.Errorf("Package = %+v, want {Name:p}", cu.Package)
	}
}

func TestParseDefaultPackageNoPackage(t *testing.T) {
	cu, err := Parse([]byte("import a.b.C;"), "T.java")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cu.Package != nil {
		t.Errorf("Package = %+v, want nil (default package)", cu.Package)
	}
	if len(cu.Imports) != 1 || cu.Imports[0].Path != "a.b.C" {
		t.Errorf("imports = %+v, want [a.b.C]", cu.Imports)
	}
}

func TestParseImportContextualKeywordSegments(t *testing.T) {
	// codex review #2:Java 包名常含 contextual keyword(record / sealed / var 等)
	src := `import com.acme.record.UserDO;
import com.thfund.sales.fundsalesmrksupport.facade.model.module.X;
import java.util.var;`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantPaths := []string{
		"com.acme.record.UserDO",
		"com.thfund.sales.fundsalesmrksupport.facade.model.module.X",
		"java.util.var",
	}
	if len(cu.Imports) != len(wantPaths) {
		t.Fatalf("imports = %+v, want %d entries", cu.Imports, len(wantPaths))
	}
	for i, p := range wantPaths {
		if cu.Imports[i].Path != p {
			t.Errorf("imports[%d].Path = %q, want %q", i, cu.Imports[i].Path, p)
		}
	}
}

func TestParsePackageContextualKeywordSegments(t *testing.T) {
	cu, err := Parse([]byte("package com.acme.record.dto;"), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cu.Package == nil || cu.Package.Name != "com.acme.record.dto" {
		t.Errorf("package = %+v", cu.Package)
	}
}

func TestParseImportMalformedReturnsError(t *testing.T) {
	cases := []string{
		"import ;",            // 缺 ident
		"import a.;",          // dot 后无 ident 或 *
		"import a.b.C",        // 缺 ;
		"import static ;",     // static 后缺 ident
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			_, err := Parse([]byte(src), "T.java")
			if err == nil {
				t.Errorf("expected error, got nil for %q", src)
			}
		})
	}
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v -run TestParse`

Expected: 全 PASS(注意旧的 `TestParseEmptyReturnsEmptyCompilationUnit` 仍 PASS,因为空输入直接走 EOF 退出循环)。

- [ ] **Step 4: commit**

```bash
git add internal/javaparser/parser.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser 解析 package + 4 种 import 形态"
```

---

## Task 4:TypeRef + 完整泛型 / wildcard / array dims / type bound

**Files:**
- Create: `internal/javaparser/typeref.go`
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 创建 `typeref.go`**

```go
package javaparser

// parseTypeRef 解析一个 type reference,出现位置:
//   - method return type / parameter type / field type
//   - extends / implements / permits / throws clause 中的单个类型
//   - generic argument(嵌套泛型 / wildcard)
//   - type bound(`T extends A & B` 中的 A 和 B)
//
// 形态:
//
//	leading type-use annotations? + qualified name
//	+ optional "<" typeArgs ">"
//	+ optional "[" "]" 多对(array dims)
//
// **接受** primitive 类型 —— `int` / `void` / `boolean` 等 Java keyword 也走这里。
//
// **接受 leading type-use annotation**(codex review #9):`@NonNull String` /
// `@Min(0) int` 等。 annotation 本身解析后**丢弃**(C.3 adapter 不用 type-use
// annotation 信息);如果未来需要可在 TypeRef 加 Annotations 字段。
//
// **不消费** wildcard(`?`)—— wildcard 只在 type argument 位置合法,由
// parseTypeArgs 显式分支处理。
//
// 已知 OOS:`Outer<T>.Inner<U>` 这种 generic-qualified inner type — 当前实现在
// 解析完 `Outer<T>` 后停止,留下 `.Inner` 给上层(会失败)。 codex review #3
// 标 OOS,真业务 facade 罕见。
func parseTypeRef(c *cursor) (TypeRef, error) {
	startPos := c.pos()
	// 吃 leading type-use annotations(`@NonNull String`)
	if err := skipTypeUseAnnotations(c); err != nil {
		return TypeRef{}, err
	}
	tok := c.peek()
	if !isIdentLike(tok) && tok.Kind != TokenKeyword {
		return TypeRef{}, parseError(startPos, "expected type, got %s %q", tok.Kind, tok.Value)
	}
	c.consume()
	name := tok.Value
	// qualified name 续上(允许 contextual keyword 作为段名)
	for c.peek().Kind == TokenDot {
		next := c.peekN(1)
		if !isIdentLike(next) {
			break
		}
		c.consume() // dot
		c.consume() // ident-like
		name += "." + next.Value
	}

	ref := TypeRef{Name: name, Pos: startPos}

	// 泛型参数
	if c.peek().Kind == TokenLAngle {
		args, err := parseTypeArgs(c)
		if err != nil {
			return TypeRef{}, err
		}
		ref.Args = args
	}

	// array dims with optional interleaved type-use annotations:
	//   `String[][]` / `String @A []` / `String @A [] @B []`
	// codex review (round 2) #2:一次性 skip annotations 再 read dims 漏掉
	// 第二维之前的 annotation,改成循环。
	for {
		if c.peek().Kind == TokenAt {
			if err := skipTypeUseAnnotations(c); err != nil {
				return TypeRef{}, err
			}
		}
		if c.peek().Kind != TokenLBracket || c.peekN(1).Kind != TokenRBracket {
			break
		}
		c.consume() // [
		c.consume() // ]
		ref.ArrayDims++
	}
	return ref, nil
}

// skipTypeUseAnnotations 跳过 type-use annotation 序列(`@A @B(args) ...`)。
// 不存储,只吃 token。 用于 TypeRef leading / array-dim leading 位置。
// codex review #9。 复用 parseAnnotation 不行(decls.go 才定义,会循环依赖),
// 这里 inline 实现。
func skipTypeUseAnnotations(c *cursor) error {
	for c.peek().Kind == TokenAt {
		c.consume() // @
		// 吃 qualified annotation name
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
		// optional `(...)` balanced skip
		if c.peek().Kind == TokenLParen {
			if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseTypeArgs 解析 `<A, B, ?, ? extends X>` 的 generic argument list。
// 必须以 `<` 开头,以 `>` 结尾(单个 `>`,因为 C.1 lexer 不合并 `>>`)。
// 允许空 list `<>`(diamond)。
func parseTypeArgs(c *cursor) ([]TypeRef, error) {
	if _, err := c.expect(TokenLAngle, "<"); err != nil {
		return nil, err
	}
	var args []TypeRef
	// diamond
	if c.peek().Kind == TokenRAngle {
		c.consume()
		return args, nil
	}
	for {
		arg, err := parseTypeArgOrWildcard(c)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if c.match(TokenComma) {
			continue
		}
		break
	}
	if _, err := c.expect(TokenRAngle, ">"); err != nil {
		return nil, err
	}
	return args, nil
}

// parseTypeArgOrWildcard 解析单个 type argument,支持 wildcard 与 leading
// type-use annotation(`@NonNull String` / `@A ? extends X`)。
//
//	X / a.b.C / List<X> / X[]
//	? / ? extends X / ? super X
func parseTypeArgOrWildcard(c *cursor) (TypeRef, error) {
	startPos := c.pos()
	// leading type-use annotation 在 wildcard 与普通类型前都允许
	if err := skipTypeUseAnnotations(c); err != nil {
		return TypeRef{}, err
	}
	if c.peek().Kind == TokenQuestion {
		c.consume()
		ref := TypeRef{IsWildcard: true, WildcardKind: WildcardUnbounded, Pos: startPos}
		// 看是否 extends / super
		tok := c.peek()
		if tok.Kind == TokenKeyword && tok.Value == "extends" {
			c.consume()
			bound, err := parseTypeRef(c)
			if err != nil {
				return TypeRef{}, err
			}
			ref.WildcardKind = WildcardExtends
			ref.WildcardBound = &bound
		} else if tok.Kind == TokenKeyword && tok.Value == "super" {
			c.consume()
			bound, err := parseTypeRef(c)
			if err != nil {
				return TypeRef{}, err
			}
			ref.WildcardKind = WildcardSuper
			ref.WildcardBound = &bound
		}
		return ref, nil
	}
	return parseTypeRef(c)
}

// parseTypeParams 解析 declared type parameters:`<T, K extends A & B>`。
// 必须以 `<` 开头;允许零个参数即 `<>`(虽然 declared type params 一般不会空,容错处理)。
// 返回 nil 表示当前位置不是 `<` 起头(调用方 peek 判断)。
func parseTypeParams(c *cursor) ([]TypeParam, error) {
	if c.peek().Kind != TokenLAngle {
		return nil, nil
	}
	c.consume() // <
	var params []TypeParam
	if c.peek().Kind == TokenRAngle {
		c.consume()
		return params, nil
	}
	for {
		// 允许 annotated type parameter:`<@Nonnull T extends A>`(codex review #10)。
		// annotation 不存,只 skip。 若未来需要可加 TypeParam.Annotations 字段。
		if err := skipTypeUseAnnotations(c); err != nil {
			return nil, err
		}
		nameTok, err := c.expect(TokenIdent, "type parameter name")
		if err != nil {
			return nil, err
		}
		param := TypeParam{Name: nameTok.Value}
		// optional `extends A & B`
		if c.matchKeyword("extends") {
			for {
				bound, err := parseTypeRef(c)
				if err != nil {
					return nil, err
				}
				param.Bounds = append(param.Bounds, bound)
				if !c.match(TokenAmp) {
					break
				}
			}
		}
		params = append(params, param)
		if c.match(TokenComma) {
			continue
		}
		break
	}
	if _, err := c.expect(TokenRAngle, ">"); err != nil {
		return nil, err
	}
	return params, nil
}

// readArrayDims 读连续的 `[` `]` 对,返回对数。 当前位置不是 `[` 时返回 0。
func readArrayDims(c *cursor) int {
	n := 0
	for c.peek().Kind == TokenLBracket {
		// 必须紧跟 `]`,否则 break(不消费 `[`)
		if c.peekN(1).Kind != TokenRBracket {
			return n
		}
		c.consume() // [
		c.consume() // ]
		n++
	}
	return n
}
```

- [ ] **Step 2: 加 TypeRef test**

```go
func TestParseTypeRefBasic(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"int", "int"},
		{"String", "String"},
		{"java.util.List", "java.util.List"},
		{"List<String>", "List<String>"},
		{"Map<String, List<Long>>", "Map<String, List<Long>>"},
		{"List<?>", "List<?>"},
		{"List<? extends Number>", "List<? extends Number>"},
		{"List<? super Integer>", "List<? super Integer>"},
		{"String[]", "String[]"},
		{"String[][]", "String[][]"},
		{"List<String>[]", "List<String>[]"},
		{"Map<String, java.util.List<X>>", "Map<String, java.util.List<X>>"},
		// Note: `<>` diamond 仅在构造调用 `new List<>()` 合法,在 declaration / type
		// reference 位置非法。 parser 容错接受(空 Args),但 TypeRef.String() 是有损
		// 序列化:`List<>` 输入 → 空 Args → String() 输出 `List`(不带括号)。
		// 不在测试覆盖范围。
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, err := Tokenize([]byte(tc.src))
			if err != nil {
				t.Fatalf("tokenize: %v", err)
			}
			c := newCursor(tokens)
			ref, err := parseTypeRef(c)
			if err != nil {
				t.Fatalf("parseTypeRef: %v", err)
			}
			if ref.String() != tc.want {
				t.Errorf("String() = %q, want %q", ref.String(), tc.want)
			}
		})
	}
}

func TestParseTypeParams(t *testing.T) {
	cases := []struct {
		src       string
		wantNames []string
		wantBound map[string]string // name → bound.String()(只看第一个 bound)
	}{
		{"<T>", []string{"T"}, nil},
		{"<T, K>", []string{"T", "K"}, nil},
		{"<T extends Number>", []string{"T"}, map[string]string{"T": "Number"}},
		{"<T extends A & B>", []string{"T"}, map[string]string{"T": "A"}},
		{"<T, K extends Comparable<K>>", []string{"T", "K"}, map[string]string{"K": "Comparable<K>"}},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, _ := Tokenize([]byte(tc.src))
			c := newCursor(tokens)
			params, err := parseTypeParams(c)
			if err != nil {
				t.Fatalf("parseTypeParams: %v", err)
			}
			if len(params) != len(tc.wantNames) {
				t.Fatalf("params = %+v, want names %v", params, tc.wantNames)
			}
			for i, n := range tc.wantNames {
				if params[i].Name != n {
					t.Errorf("params[%d].Name = %q, want %q", i, params[i].Name, n)
				}
				if want, ok := tc.wantBound[n]; ok {
					if len(params[i].Bounds) == 0 {
						t.Errorf("params[%d] bound missing, want %q", i, want)
					} else if got := params[i].Bounds[0].String(); got != want {
						t.Errorf("params[%d] bound = %q, want %q", i, got, want)
					}
				}
			}
		})
	}
}

func TestParseTypeRefArrayDimsBoundary(t *testing.T) {
	// `String[ ]` (中间空格)应该正常识别为 1 维
	tokens, _ := Tokenize([]byte("String[ ]"))
	c := newCursor(tokens)
	ref, err := parseTypeRef(c)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ref.ArrayDims != 1 {
		t.Errorf("dims = %d, want 1", ref.ArrayDims)
	}
}

func TestParseTypeRefTypeUseAnnotations(t *testing.T) {
	// codex review #9:type-use annotation 在 type ref / generic arg 位置
	cases := []struct {
		src  string
		want string
	}{
		{"@NonNull String", "String"},
		{"@Min(0) int", "int"},
		{"List<@NonNull String>", "List<String>"},
		{"Map<@Key String, @Val Foo>", "Map<String, Foo>"},
		{"@A String @B []", "String[]"},
		{"List<@A ? extends @B Number>", "List<? extends Number>"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, err := Tokenize([]byte(tc.src))
			if err != nil {
				t.Fatalf("tokenize: %v", err)
			}
			c := newCursor(tokens)
			ref, err := parseTypeRef(c)
			if err != nil {
				t.Fatalf("parseTypeRef: %v", err)
			}
			if ref.String() != tc.want {
				t.Errorf("String() = %q, want %q (annotation skipped)", ref.String(), tc.want)
			}
		})
	}
}

func TestParseTypeRefContextualKeywordInQualifiedName(t *testing.T) {
	// codex review #2:Java 真实代码 `com.acme.record.User` 中 record 是 contextual keyword
	cases := []struct {
		src  string
		want string
	}{
		{"com.acme.record.User", "com.acme.record.User"},
		{"java.util.module.X", "java.util.module.X"},
		{"List<com.acme.record.User>", "List<com.acme.record.User>"},
		{"sealed", "sealed"}, // 单独 contextual keyword 当类型名也可以
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, _ := Tokenize([]byte(tc.src))
			c := newCursor(tokens)
			ref, err := parseTypeRef(c)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if ref.String() != tc.want {
				t.Errorf("String() = %q, want %q", ref.String(), tc.want)
			}
		})
	}
}

func TestParseTypeParamsAnnotated(t *testing.T) {
	// codex review #10
	tokens, _ := Tokenize([]byte("<@Nonnull T, @A K extends @B Comparable<K>>"))
	c := newCursor(tokens)
	params, err := parseTypeParams(c)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(params) != 2 || params[0].Name != "T" || params[1].Name != "K" {
		t.Errorf("params = %+v", params)
	}
	if len(params[1].Bounds) != 1 || params[1].Bounds[0].String() != "Comparable<K>" {
		t.Errorf("K bound = %+v", params[1].Bounds)
	}
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v -run 'TestParseTypeRef|TestParseTypeParams'`

Expected: 全 PASS。 重点验证嵌套泛型(`Map<String, List<Long>>` 的两个 `>>` 由 C.1 lexer 拆成 2 个 TokenRAngle,parser 在两层递归各自 expect 一个)、wildcard 三种形态、array dims、diamond。

- [ ] **Step 4: commit**

```bash
git add internal/javaparser/typeref.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser 解析 TypeRef 完整泛型 / wildcard / array / type bound"
```

---

## Task 5:TypeDecl 顶层(class / interface / enum / record / @interface)+ preamble + declared type params + extends/implements/permits + non-sealed

**Files:**
- Create: `internal/javaparser/decls.go`
- Modify: `internal/javaparser/parser.go`(在 `parseCompilationUnit` 主循环里接入 `parseTypeDecl`)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 创建 `decls.go`**

```go
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
		c.consume()       // dot
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

// parseTypeBody dispatcher。 enum / annotation / 普通 class/interface/record 走不同分支。
// Task 5 阶段只 stub:吃掉所有 token 到匹配的 `}` 之前(parser_test 用 brace 平衡判断成功)。
// Task 6+ 会把它替换为真实 member dispatch。
func parseTypeBody(c *cursor, decl *TypeDecl) error {
	// Task 5 stub:平衡 skip 到 RBrace(不消费 RBrace,留给 caller expect)
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
	return parseError(c.pos(), "unexpected EOF in type body")
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
```

- [ ] **Step 2: 修改 `parser.go` 把 `parseCompilationUnit` 主循环里 `c.consume()` 占位替换为 `parseTypeDecl`**

在 `parser.go` 找到 `parseCompilationUnit` 函数,把末尾的占位 `c.consume()` 替换为:

```go
		// 否则期望一个 type declaration
		decl, err := parseTypeDecl(c)
		if err != nil {
			return nil, err
		}
		cu.Types = append(cu.Types, decl)
```

这样替换之后,完整循环体看起来:

```go
	for !c.eof() {
		tok := c.peek()

		// `@` + ... + `package` = file-level annotation,skip
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
```

注意:`@` 的歧义已经在 Task 3 的 `peekAnnotationsLeadToKeyword` helper 里解决 —— 如果 `@` 后面是 `package`,走 file-level annotation skip;否则(`@Deprecated public interface Foo {}` 这类)直接进 `parseTypeDecl`,由 `parsePreamble` 把 annotation 当成 type-decl preamble 收集。

- [ ] **Step 3: 加 TypeDecl 测试**

```go
func TestParseTypeDeclTopLevelClassInterfaceEnumRecord(t *testing.T) {
	src := `package p;
public abstract class Foo<T extends Number, K> extends BaseX implements I1, I2<String> {}
public interface Bar<T> extends Comparable<T> {}
public enum Color {}
public record Point(int x, int y) {}
public @interface Marker {}
`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 5 {
		t.Fatalf("types len = %d, want 5: %+v", len(cu.Types), cu.Types)
	}
	cases := []struct {
		idx       int
		kind      TypeKind
		name      string
		modifiers []string
		nParams   int
	}{
		{0, TypeKindClass, "Foo", []string{"public", "abstract"}, 2},
		{1, TypeKindInterface, "Bar", []string{"public"}, 1},
		{2, TypeKindEnum, "Color", []string{"public"}, 0},
		{3, TypeKindRecord, "Point", []string{"public"}, 0},
		{4, TypeKindAnnotation, "Marker", []string{"public"}, 0},
	}
	for _, tc := range cases {
		got := cu.Types[tc.idx]
		if got.Kind != tc.kind || got.Name != tc.name {
			t.Errorf("types[%d] = {%s, %s}, want {%s, %s}", tc.idx, got.Kind, got.Name, tc.kind, tc.name)
		}
		if !sliceEq(got.Modifiers, tc.modifiers) {
			t.Errorf("types[%d].Modifiers = %v, want %v", tc.idx, got.Modifiers, tc.modifiers)
		}
		if len(got.TypeParams) != tc.nParams {
			t.Errorf("types[%d].TypeParams = %+v, want len %d", tc.idx, got.TypeParams, tc.nParams)
		}
	}

	// Foo: extends + implements
	foo := cu.Types[0]
	if len(foo.Extends) != 1 || foo.Extends[0].String() != "BaseX" {
		t.Errorf("Foo.Extends = %+v", foo.Extends)
	}
	if len(foo.Implements) != 2 || foo.Implements[0].String() != "I1" || foo.Implements[1].String() != "I2<String>" {
		t.Errorf("Foo.Implements = %+v", foo.Implements)
	}
	if foo.TypeParams[0].Name != "T" || len(foo.TypeParams[0].Bounds) != 1 || foo.TypeParams[0].Bounds[0].String() != "Number" {
		t.Errorf("Foo.TypeParams[0] = %+v", foo.TypeParams[0])
	}
}

func TestParseTypeDeclSealedAndNonSealed(t *testing.T) {
	src := `sealed class Shape permits Circle, Square {}
non-sealed class Circle extends Shape {}
final class Square extends Shape {}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 3 {
		t.Fatalf("types = %d", len(cu.Types))
	}
	if !sliceEq(cu.Types[0].Modifiers, []string{"sealed"}) {
		t.Errorf("Shape.Modifiers = %v", cu.Types[0].Modifiers)
	}
	if len(cu.Types[0].Permits) != 2 {
		t.Errorf("Shape.Permits = %+v", cu.Types[0].Permits)
	}
	if !sliceEq(cu.Types[1].Modifiers, []string{"non-sealed"}) {
		t.Errorf("Circle.Modifiers = %v, want [non-sealed]", cu.Types[1].Modifiers)
	}
}

func TestParseTypeDeclNonSealedRequiresAdjacency(t *testing.T) {
	// codex review #6:`non - sealed`(带空格)不是合法 Java,parser 不应合成。
	// parsePreamble 在三 token 之间用 Token.Off 校验相邻;有空格时不合并。
	src := `non - sealed class Bad {}`
	cu, err := Parse([]byte(src), "T.java")
	// 不合并意味着 `non` 不是 modifier,会进 parseTypeDecl 期望 type keyword 然后失败。
	// 我们 expect error,而不是 silently 当成 non-sealed class Bad。
	if err == nil {
		t.Fatalf("expected error for non-adjacent 'non - sealed', got types=%+v", cu.Types)
	}
}

func TestParseTypeDeclIdentifierNamedNon(t *testing.T) {
	// 另一面:类型本身叫 `non` 不影响别的(虽然不推荐,但合法)
	src := `package p; class Foo { private int x; }`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 1 || cu.Types[0].Name != "Foo" {
		t.Errorf("types = %+v", cu.Types)
	}
}

func TestParseTypeDeclWithAnnotationsAndJavadoc(t *testing.T) {
	src := `package p;

/** 资产查询门面。 */
@Deprecated
@SuppressWarnings("all")
public interface AssetFacade {}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 1 {
		t.Fatalf("types = %d", len(cu.Types))
	}
	td := cu.Types[0]
	if !contains(td.Javadoc, "资产查询门面") {
		t.Errorf("Javadoc = %q", td.Javadoc)
	}
	if len(td.Annotations) != 2 {
		t.Errorf("Annotations = %+v", td.Annotations)
	}
	if td.Annotations[0].Name != "Deprecated" || td.Annotations[1].Name != "SuppressWarnings" {
		t.Errorf("Annotations names = %+v", td.Annotations)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/javaparser/ -v -run TestParseTypeDecl`

Expected: 全 PASS。 body 内容 Task 5 阶段还是空(Methods/Fields 都是 nil),Task 6+ 接入。

- [ ] **Step 5: commit**

```bash
git add internal/javaparser/decls.go internal/javaparser/parser.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser 解析顶层 type declaration(含 sealed/non-sealed)"
```

---

## Task 6:Class / interface body 成员 dispatch(method vs field vs nested type vs ctor vs static init)

**Files:**
- Modify: `internal/javaparser/decls.go`(替换 Task 5 的 `parseTypeBody` stub)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 在 `decls.go` 替换 `parseTypeBody` 函数**

把 Task 5 留的 stub 替换为真实 dispatcher。 注意:enum body / annotation body 走不同函数,在 Task 9 才接入;Task 6 只覆盖 class / interface / record body。

```go
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
		// 允许 body 内独立的 `;`(empty declaration,JLS 允许)
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
//
//	1. peek 是 type keyword (class/interface/enum/record) 或 `@interface` → nested type
//	2. preamble 之后,识别 ctor / method / field / nested type:
//	   - 先解析 preamble(modifiers + annotations + javadoc)
//	   - 再看接下来的 token 序列决定
//	   - static initializer block(`static { ... }`)直接 skip(modifiers 里含 "static" 且 peek 是 `{`)
//	   - instance initializer block(直接 `{ ... }`,没 modifier)在 preamble 之后 peek 是 `{` 也 skip
//	   - 否则在 preamble 之后可能是 generic method `<T> T foo(...)` 或者直接 type
//	   - 用 `looksLikeConstructor` / `looksLikeMethod` 决定 dispatch
func parseMember(c *cursor, owner *TypeDecl) error {
	// Nested type 不带 preamble 的快速路径(public/private/static 等会走下面的 preamble)
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

	// preamble 之后:可能是 nested type / method / field / initializer block
	if peekIsNestedTypeStart(c) {
		// reconstruct nested type with preamble
		nested, err := parseTypeDeclWithPreamble(c, pre)
		if err != nil {
			return err
		}
		owner.NestedTypes = append(owner.NestedTypes, nested)
		return nil
	}

	// initializer block:peek 是 `{`(可能前面有 `static` modifier;也可能完全 anonymous)
	if c.peek().Kind == TokenLBrace {
		if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
			return err
		}
		return nil
	}

	// method or field:都从 type-params? + type-ref 开始,在 `(` 处决定
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
// 为复用方便,实现成 inline 版本(轻微 duplicate)。
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
		c.consume() // @
		c.consume() // interface
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

// parseMethodOrField stub for Task 6:解析 generic-method-typeparams? + ReturnType +
// 在 `(` 处决定 method,在 `;` / `=` / `,` / `[` 处决定 field。 ctor 单独识别。
// **实际实现** Task 7(method)+ Task 8(field)。 Task 6 阶段先用 brace/paren/semicolon
// 平衡 skip 占位 —— 这样 type body 能 dispatch 走通,member 计数为 0。
func parseMethodOrField(c *cursor, pre preamble, owner *TypeDecl) (*MethodDecl, []FieldDecl, error) {
	// 平衡 skip 到下一个 `;` 或 `}`(member 边界);遇到 `(` 或 `{` 平衡跳过整段。
	for !c.eof() {
		tok := c.peek()
		switch tok.Kind {
		case TokenLParen:
			if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
				return nil, nil, err
			}
		case TokenLBrace:
			if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
				return nil, nil, err
			}
			// body 结束意味着 member 结束(method)
			return nil, nil, nil
		case TokenSemicolon:
			c.consume()
			return nil, nil, nil
		case TokenRBrace:
			return nil, nil, nil
		default:
			c.consume()
		}
	}
	return nil, nil, nil
}
```

- [ ] **Step 2: 加 body dispatch 测试**

```go
func TestParseTypeBodyDispatchSkipsMembersAndNested(t *testing.T) {
	src := `package p;
public class Outer {
    private int x;
    public Outer(int x) { this.x = x; }
    public void greet() { System.out.println("hi"); }
    static { /* static init */ }
    {  /* instance init */ }
    static class Inner {}
    record Point(int x, int y) {}
}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 1 {
		t.Fatalf("types = %d", len(cu.Types))
	}
	outer := cu.Types[0]
	if outer.Name != "Outer" || outer.Kind != TypeKindClass {
		t.Errorf("Outer = %+v", outer)
	}
	if len(outer.NestedTypes) != 2 {
		t.Fatalf("NestedTypes len = %d, want 2", len(outer.NestedTypes))
	}
	if outer.NestedTypes[0].Name != "Inner" || outer.NestedTypes[0].Kind != TypeKindClass {
		t.Errorf("Inner = %+v", outer.NestedTypes[0])
	}
	if outer.NestedTypes[1].Name != "Point" || outer.NestedTypes[1].Kind != TypeKindRecord {
		t.Errorf("Point = %+v", outer.NestedTypes[1])
	}
	// Task 6 阶段 Methods / Fields 还是 nil(Task 7+ 接入)。 这里不 assert 内容,
	// 只确保 parse 没炸出 error 且 nested type 正确识别。
}

func TestParseTypeBodyEmpty(t *testing.T) {
	cu, err := Parse([]byte("class Empty {}"), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 1 || cu.Types[0].Name != "Empty" {
		t.Errorf("types = %+v", cu.Types)
	}
}

func TestParseTypeBodyEnumStubSkips(t *testing.T) {
	cu, err := Parse([]byte("enum Color { RED, GREEN, BLUE; public String code() { return name(); } }"), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 1 || cu.Types[0].Kind != TypeKindEnum {
		t.Errorf("types = %+v", cu.Types)
	}
	// Task 9 才有 EnumValues 内容,这里只确认 parse 不炸。
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v -run TestParseTypeBody`

Expected: 全 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/javaparser/decls.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser type body dispatcher,识别 nested type / initializer block"
```

---

## Task 7:MethodDecl 完整解析(含 ctor / 泛型方法 / 参数 / varargs / throws / body skip)

**Files:**
- Modify: `internal/javaparser/decls.go`(替换 Task 6 的 `parseMethodOrField` 占位)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 把 `parseMethodOrField` 替换为真实实现**

```go
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

	// `(` → method;else → field
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

	// annotation `default <expr>` 也可能出现(C.2 阶段没 annotation body,但 default
	// 可能出现在普通 interface 的 default method 之外 —— 不,interface default 是 modifier。
	// annotation type 的 default 在 Task 9 处理)。
	// 这里只识别 method body 或 `;`。
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
			// skip until `;`
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

// finishFieldDecl Task 7 占位:只处理单字段 + skip initializer + 吃 `;`。
// Task 8 替换为完整 multi-decl 处理。
func finishFieldDecl(c *cursor, pre preamble, typ TypeRef, name string, startPos Position) ([]FieldDecl, error) {
	field := FieldDecl{
		Modifiers:   pre.Modifiers,
		Annotations: pre.Annotations,
		Javadoc:     pre.Javadoc,
		Type:        typ,
		Name:        name,
		Pos:         startPos,
	}
	// C-style array dim on name
	if c.peek().Kind == TokenLBracket {
		field.Type.ArrayDims += readArrayDims(c)
	}
	if c.match(TokenAssign) {
		if err := skipFieldInitializer(c); err != nil {
			return nil, err
		}
	}
	if _, err := c.expect(TokenSemicolon, ";"); err != nil {
		return nil, err
	}
	return []FieldDecl{field}, nil
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
```

- [ ] **Step 2: 加 MethodDecl 测试**

```go
func TestParseMethodSignatures(t *testing.T) {
	src := `package p;
public interface Foo {
    /** 第一个方法 */
    String hello();
    int add(int x, int y);
    <T> List<T> wrap(T item);
    void greet(String... names);
    void fail() throws java.io.IOException, RuntimeException;
    Map<String, List<Long>> findAll(@NotNull String key, final int limit);
}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 1 {
		t.Fatalf("types = %d", len(cu.Types))
	}
	methods := cu.Types[0].Methods
	if len(methods) != 6 {
		t.Fatalf("methods = %d, want 6: %+v", len(methods), methods)
	}

	m0 := methods[0]
	if m0.Name != "hello" || m0.ReturnType.String() != "String" || !contains(m0.Javadoc, "第一个方法") {
		t.Errorf("hello = %+v", m0)
	}

	m1 := methods[1]
	if m1.Name != "add" || len(m1.Params) != 2 ||
		m1.Params[0].Type.String() != "int" || m1.Params[0].Name != "x" ||
		m1.Params[1].Type.String() != "int" || m1.Params[1].Name != "y" {
		t.Errorf("add = %+v", m1)
	}

	m2 := methods[2]
	if m2.Name != "wrap" || len(m2.TypeParams) != 1 || m2.TypeParams[0].Name != "T" {
		t.Errorf("wrap.TypeParams = %+v", m2.TypeParams)
	}
	if m2.ReturnType.String() != "List<T>" {
		t.Errorf("wrap.ReturnType = %q", m2.ReturnType.String())
	}

	m3 := methods[3]
	if m3.Name != "greet" || len(m3.Params) != 1 || !m3.Params[0].IsVarargs ||
		m3.Params[0].Type.String() != "String[]" {
		t.Errorf("greet = %+v", m3)
	}

	m4 := methods[4]
	if m4.Name != "fail" || len(m4.Throws) != 2 ||
		m4.Throws[0].String() != "java.io.IOException" || m4.Throws[1].String() != "RuntimeException" {
		t.Errorf("fail.Throws = %+v", m4.Throws)
	}

	m5 := methods[5]
	if m5.Name != "findAll" {
		t.Errorf("findAll name = %q", m5.Name)
	}
	if m5.ReturnType.String() != "Map<String, List<Long>>" {
		t.Errorf("findAll.ReturnType = %q", m5.ReturnType.String())
	}
	if len(m5.Params) != 2 ||
		m5.Params[0].Type.String() != "String" || m5.Params[0].Name != "key" ||
		len(m5.Params[0].Annotations) != 1 || m5.Params[0].Annotations[0].Name != "NotNull" ||
		m5.Params[1].Type.String() != "int" || m5.Params[1].Name != "limit" ||
		!m5.Params[1].Final {
		t.Errorf("findAll.Params = %+v", m5.Params)
	}
}

func TestParseConstructor(t *testing.T) {
	src := `class Foo {
		public Foo() {}
		public Foo(int x) { this.x = x; }
		Foo(String s) throws RuntimeException { /* body */ }
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	methods := cu.Types[0].Methods
	if len(methods) != 3 {
		t.Fatalf("methods = %d", len(methods))
	}
	for i, m := range methods {
		if !m.IsConstructor {
			t.Errorf("methods[%d].IsConstructor = false, want true", i)
		}
		if m.Name != "Foo" {
			t.Errorf("methods[%d].Name = %q, want Foo", i, m.Name)
		}
	}
	if len(methods[2].Throws) != 1 || methods[2].Throws[0].String() != "RuntimeException" {
		t.Errorf("ctor[2].Throws = %+v", methods[2].Throws)
	}
}

func TestParseMethodAbstractAndDefaultModifiers(t *testing.T) {
	src := `interface Foo {
		void abstractMethod();
		default String greet() { return "hi"; }
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	methods := cu.Types[0].Methods
	if len(methods) != 2 {
		t.Fatalf("methods = %d", len(methods))
	}
	if methods[0].Name != "abstractMethod" {
		t.Errorf("methods[0] = %+v", methods[0])
	}
	if methods[1].Name != "greet" || !sliceEq(methods[1].Modifiers, []string{"default"}) {
		t.Errorf("methods[1] = %+v", methods[1])
	}
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v -run TestParseMethod`

Expected: 全 PASS。 重点验证泛型方法、varargs、throws clause、嵌套泛型 ReturnType、annotation + final 在参数位置、ctor 识别。

- [ ] **Step 4: commit**

```bash
git add internal/javaparser/decls.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser 解析 method / constructor declaration"
```

---

## Task 8:FieldDecl multi-decl 展开 + 边界 case

**Files:**
- Modify: `internal/javaparser/decls.go`(替换 `finishFieldDecl`)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 替换 `finishFieldDecl` 处理 `int a, b = 1, c[];` 形态**

```go
// finishFieldDecl 处理一个 field declaration,允许 multi-decl(`int a, b = 1, c[];`)。
// 输入:已经消费完 type 和 第一个 name,startPos 是 type 位置。
// 输出:展开成多个 FieldDecl,每个 share modifiers/annotations/javadoc/type(type 可能因 per-name `[]` 调整 ArrayDims)。
func finishFieldDecl(c *cursor, pre preamble, typ TypeRef, firstName string, startPos Position) ([]FieldDecl, error) {
	var fields []FieldDecl
	curName := firstName
	curType := typ
	// per-name `[]`
	if c.peek().Kind == TokenLBracket {
		curType.ArrayDims += readArrayDims(c)
	}
	// optional initializer
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
	// 继续 multi-decl
	for c.match(TokenComma) {
		curType = typ // 每个新名字从 base type 重新开始(C-style `[]` 是 per-name)
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
```

- [ ] **Step 2: 加 FieldDecl 测试**

```go
func TestParseFieldSingleAndMulti(t *testing.T) {
	src := `class Foo {
		private int x;
		public String name = "default";
		protected final long a = 1L, b, c = 3L;
		String[] xs;
		int matrix[][];
		private static final java.util.List<String> NAMES = java.util.Arrays.asList("a", "b");
		@JsonProperty("display") private String display;
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	fields := cu.Types[0].Fields
	// x; name; a; b; c; xs; matrix; NAMES; display → 9 个
	if len(fields) != 9 {
		t.Fatalf("fields len = %d, want 9: %+v", len(fields), fields)
	}

	// 抽样:multi-decl 3 个共享 modifiers
	for i := 2; i <= 4; i++ {
		if !sliceEq(fields[i].Modifiers, []string{"protected", "final"}) {
			t.Errorf("fields[%d].Modifiers = %v", i, fields[i].Modifiers)
		}
		if fields[i].Type.String() != "long" {
			t.Errorf("fields[%d].Type = %q", i, fields[i].Type.String())
		}
	}
	if fields[2].Name != "a" || fields[3].Name != "b" || fields[4].Name != "c" {
		t.Errorf("multi-decl names = %s/%s/%s", fields[2].Name, fields[3].Name, fields[4].Name)
	}

	// 数组维度
	if fields[5].Type.String() != "String[]" {
		t.Errorf("xs.Type = %q", fields[5].Type.String())
	}
	if fields[6].Type.String() != "int[][]" {
		t.Errorf("matrix.Type = %q (C-style dims)", fields[6].Type.String())
	}

	// 泛型 + initializer skip
	if fields[7].Name != "NAMES" || fields[7].Type.String() != "java.util.List<String>" {
		t.Errorf("NAMES = %+v", fields[7])
	}

	// annotation
	if len(fields[8].Annotations) != 1 || fields[8].Annotations[0].Name != "JsonProperty" {
		t.Errorf("display.Annotations = %+v", fields[8].Annotations)
	}
}

func TestParseFieldInitializerComplexSkip(t *testing.T) {
	// 初始化器含 lambda / 嵌套调用 / array literal / 字符串 — 全 skip
	src := `class Foo {
		private Runnable r = () -> { System.out.println("hi"); };
		private int[] xs = new int[]{1, 2, 3};
		private String s = "with semicolon ; inside";
		private int x = 1;
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	fields := cu.Types[0].Fields
	if len(fields) != 4 {
		t.Fatalf("fields = %d, want 4 (initializer skip 不应误吃 ;)", len(fields))
	}
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/javaparser/ -v -run TestParseField`

Expected: 全 PASS。 注意 string literal 内的 `;` 不会被误吃(C.1 lexer 把字符串当一个 token,parser 看不到内部字符);initializer 内 lambda body 的 `{}` 被 brace balanced skip 正确穿越。

- [ ] **Step 4: commit**

```bash
git add internal/javaparser/decls.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser field declaration multi-decl + initializer skip"
```

---

## Task 9:Enum body + Record header + Annotation declaration body

**Files:**
- Modify: `internal/javaparser/decls.go`(替换 `parseTypeBody` 的 enum 分支、替换 `parseRecordHeader`、加 annotation body 分支)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 替换 `parseRecordHeader` 与 `parseTypeBody` 的 enum/annotation 分支**

把 Task 5 的 `parseRecordHeader` 替换为复用 `parseParamList`:

```go
// parseRecordHeader 解析 `record Point(int x, String name)` 中的 `(...)`。
// 复用 parseParamList(record component 形态与 method param 相同)。
func parseRecordHeader(c *cursor) ([]ParamDecl, error) {
	return parseParamList(c)
}
```

然后把 `parseTypeBody` 替换为完整 dispatcher:

```go
func parseTypeBody(c *cursor, decl *TypeDecl) error {
	switch decl.Kind {
	case TypeKindEnum:
		return parseEnumBody(c, decl)
	case TypeKindAnnotation:
		// annotation body 跟 class/interface body 形态接近,但 method 可能有
		// `default <literal>;`。 finishMethodDecl 已经容忍 `default` token,
		// 直接走通用 member dispatch 即可。
		return parseClassBodyMembers(c, decl)
	default:
		return parseClassBodyMembers(c, decl)
	}
}

// parseClassBodyMembers 把 Task 6 的 dispatcher 主循环抽出来命名,parseTypeBody 与
// annotation body 复用。
func parseClassBodyMembers(c *cursor, decl *TypeDecl) error {
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

// parseEnumBody 解析 enum body:
//
//	(EnumConstant (',' EnumConstant)* (',')?)? (';' ClassBodyDeclaration*)?
//
// 形态:
//   - 0+ 个 enum constant,逗号分隔,最后允许 trailing comma
//   - 可选 `;`,之后是普通 class body(method / field / nested type)
//   - 没有 `;` 时 body 直接以 `}` 结束
//
// 每个 enum constant:可选 annotation,然后 Ident,然后可选 `(...)`(ctor args,skip),
// 然后可选 `{...}`(anonymous class body,skip)。
func parseEnumBody(c *cursor, decl *TypeDecl) error {
	// 先解析 enum values
	for {
		if c.peek().Kind == TokenRBrace || c.peek().Kind == TokenSemicolon || c.eof() {
			break
		}
		// codex review #5:必须先 capture javadoc,后消费 annotation。
		// 否则 `/** doc */ @A RED` 会丢 doc(annotation token 挡住 peekJavadoc 回溯)。
		jdoc := cleanJavadocText(c.peekJavadoc())
		// optional annotations on value
		var anns []Annotation
		for c.peek().Kind == TokenAt {
			ann, err := parseAnnotation(c)
			if err != nil {
				return err
			}
			anns = append(anns, ann)
		}
		nameTok, err := expectIdentLike(c, "enum constant name")
		if err != nil {
			return err
		}
		ev := EnumValue{
			Annotations: anns,
			Javadoc:     jdoc,
			Name:        nameTok.Value,
			Pos:         tokenPos(nameTok),
		}
		// optional ctor args
		if c.peek().Kind == TokenLParen {
			if err := c.skipBalanced(TokenLParen, TokenRParen); err != nil {
				return err
			}
		}
		// optional anonymous class body
		if c.peek().Kind == TokenLBrace {
			if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
				return err
			}
		}
		decl.EnumValues = append(decl.EnumValues, ev)
		if !c.match(TokenComma) {
			break
		}
		// trailing comma allowed:下一轮看到 ; 或 } 即退出
	}
	// optional `;` 后接 class body
	if c.match(TokenSemicolon) {
		return parseClassBodyMembers(c, decl)
	}
	return nil
}
```

- [ ] **Step 2: 加 enum / record / annotation 测试**

```go
func TestParseEnumValuesSimple(t *testing.T) {
	src := `enum Color { RED, GREEN, BLUE }`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	enum := cu.Types[0]
	if enum.Kind != TypeKindEnum {
		t.Fatalf("kind = %s", enum.Kind)
	}
	names := []string{}
	for _, v := range enum.EnumValues {
		names = append(names, v.Name)
	}
	if !sliceEq(names, []string{"RED", "GREEN", "BLUE"}) {
		t.Errorf("enum values = %v", names)
	}
}

func TestParseEnumWithCtorArgsAndMethods(t *testing.T) {
	src := `enum Status {
		OK("ok", 0),
		FAIL("fail", -1) { @Override public String code() { return "X"; } },
		;
		private final String label;
		private final int value;
		Status(String label, int value) { this.label = label; this.value = value; }
		public String code() { return label; }
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	enum := cu.Types[0]
	names := []string{}
	for _, v := range enum.EnumValues {
		names = append(names, v.Name)
	}
	if !sliceEq(names, []string{"OK", "FAIL"}) {
		t.Errorf("enum values = %v", names)
	}
	if len(enum.Fields) != 2 {
		t.Errorf("fields = %+v", enum.Fields)
	}
	if len(enum.Methods) != 2 {
		t.Errorf("methods = %+v", enum.Methods)
	}
}

func TestParseEnumEmpty(t *testing.T) {
	src := `enum Empty {}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	enum := cu.Types[0]
	if len(enum.EnumValues) != 0 || len(enum.Fields) != 0 {
		t.Errorf("empty enum has stuff: %+v", enum)
	}
}

func TestParseRecordHeaderAndBody(t *testing.T) {
	// 覆盖 record components + compact ctor(带 modifier 与无 modifier 两种形态)+
	// 普通 method。 compact ctor 是 Step 3 强制识别(codex review #7/#8)。
	src := `record Point(int x, int y) {
		public Point {
			if (x < 0) throw new IllegalArgumentException();
		}
		public int sum() { return x + y; }
	}
	record Unprefixed(int a) {
		Unprefixed {}
		public int doubled() { return a * 2; }
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cu.Types) != 2 {
		t.Fatalf("types = %d, want 2", len(cu.Types))
	}
	rec := cu.Types[0]
	if rec.Kind != TypeKindRecord {
		t.Fatalf("kind = %s", rec.Kind)
	}
	if len(rec.RecordComponents) != 2 {
		t.Fatalf("components = %+v", rec.RecordComponents)
	}
	if rec.RecordComponents[0].Name != "x" || rec.RecordComponents[0].Type.String() != "int" {
		t.Errorf("comp[0] = %+v", rec.RecordComponents[0])
	}
	if rec.RecordComponents[1].Name != "y" || rec.RecordComponents[1].Type.String() != "int" {
		t.Errorf("comp[1] = %+v", rec.RecordComponents[1])
	}
	// compact ctor 被 Step 3 的 special path skip,不产 method;sum() 应在 Methods
	if len(rec.Methods) != 1 || rec.Methods[0].Name != "sum" {
		t.Errorf("Point.Methods = %+v, want [sum]", rec.Methods)
	}

	// 无 modifier 的 compact ctor 也必须 skip 干净
	rec2 := cu.Types[1]
	if rec2.Kind != TypeKindRecord || rec2.Name != "Unprefixed" {
		t.Fatalf("rec2 = %+v", rec2)
	}
	if len(rec2.RecordComponents) != 1 || rec2.RecordComponents[0].Name != "a" {
		t.Errorf("Unprefixed.RecordComponents = %+v", rec2.RecordComponents)
	}
	if len(rec2.Methods) != 1 || rec2.Methods[0].Name != "doubled" {
		t.Errorf("Unprefixed.Methods = %+v, want [doubled]", rec2.Methods)
	}
}

func TestParseEnumBodyNestedTypes(t *testing.T) {
	// codex review #4:enum body 内的 nested type(class / enum / record)
	src := `enum E {
		A, B;
		static class Helper {
			public int compute() { return 0; }
		}
		enum Sub { X, Y }
		record Pair(int a, int b) {}
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	e := cu.Types[0]
	if e.Kind != TypeKindEnum {
		t.Fatalf("kind = %s", e.Kind)
	}
	names := []string{}
	for _, v := range e.EnumValues {
		names = append(names, v.Name)
	}
	if !sliceEq(names, []string{"A", "B"}) {
		t.Errorf("enum values = %v", names)
	}
	if len(e.NestedTypes) != 3 {
		t.Fatalf("NestedTypes = %+v, want 3", e.NestedTypes)
	}
	wantKinds := []TypeKind{TypeKindClass, TypeKindEnum, TypeKindRecord}
	wantNames := []string{"Helper", "Sub", "Pair"}
	for i, n := range wantNames {
		if e.NestedTypes[i].Name != n || e.NestedTypes[i].Kind != wantKinds[i] {
			t.Errorf("NestedTypes[%d] = {%s, %s}, want {%s, %s}",
				i, e.NestedTypes[i].Name, e.NestedTypes[i].Kind, n, wantKinds[i])
		}
	}
}

func TestParseEnumConstantJavadocWithAnnotation(t *testing.T) {
	// codex review #5:javadoc 必须在 annotation 之前 capture
	src := `enum E {
		/** alpha doc */
		@Deprecated
		ALPHA,
		/** beta doc */
		BETA
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	e := cu.Types[0]
	if len(e.EnumValues) != 2 {
		t.Fatalf("values = %+v", e.EnumValues)
	}
	if !contains(e.EnumValues[0].Javadoc, "alpha doc") {
		t.Errorf("ALPHA.Javadoc = %q, want contains 'alpha doc'", e.EnumValues[0].Javadoc)
	}
	if len(e.EnumValues[0].Annotations) != 1 || e.EnumValues[0].Annotations[0].Name != "Deprecated" {
		t.Errorf("ALPHA.Annotations = %+v", e.EnumValues[0].Annotations)
	}
	if !contains(e.EnumValues[1].Javadoc, "beta doc") {
		t.Errorf("BETA.Javadoc = %q", e.EnumValues[1].Javadoc)
	}
}

func TestParseAnnotationDeclarationBody(t *testing.T) {
	src := `public @interface Cacheable {
		String key() default "";
		int ttlSeconds() default 60;
		String[] tags() default {};
	}`
	cu, err := Parse([]byte(src), "T.java")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	ann := cu.Types[0]
	if ann.Kind != TypeKindAnnotation {
		t.Fatalf("kind = %s", ann.Kind)
	}
	if len(ann.Methods) != 3 {
		t.Fatalf("annotation methods = %+v", ann.Methods)
	}
	wantNames := []string{"key", "ttlSeconds", "tags"}
	for i, m := range ann.Methods {
		if m.Name != wantNames[i] {
			t.Errorf("ann.Methods[%d].Name = %q, want %q", i, m.Name, wantNames[i])
		}
	}
	if ann.Methods[2].ReturnType.String() != "String[]" {
		t.Errorf("tags ReturnType = %q", ann.Methods[2].ReturnType.String())
	}
}
```

- [ ] **Step 3: 强制加 record compact ctor 识别(codex review #7/#8)**

在 `parseMethodOrField` 的 ctor 快路径(Task 7)之前**必须**插入以下代码 —— compact ctor `public R {}` / `R {}` 是 record 语法的标准形态,不能当 known limitation:

```go
	// compact ctor(record only):`R { ... }` / `public R { ... }`,无参数列表。
	// 注意不检查 pre.Modifiers —— 即使没 modifier 也是合法 compact ctor。
	// 必须在普通 ctor 快路径之前,否则会被 parseTypeRef 当 ReturnType 误吃。
	if owner.Kind == TypeKindRecord {
		if tok := c.peek(); tok.Kind == TokenIdent && tok.Value == owner.Name && c.peekN(1).Kind == TokenLBrace {
			c.consume() // ctor name
			if err := c.skipBalanced(TokenLBrace, TokenRBrace); err != nil {
				return nil, nil, err
			}
			return nil, nil, nil
		}
	}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/javaparser/ -v -run 'TestParseEnum|TestParseRecord|TestParseAnnotation'`

Expected: 全 PASS,包含 compact ctor(modifier 与无 modifier 两种形态)。 若失败,检查 Step 3 的插入位置是否在 Task 7 ctor 快路径之前(而不是之后,否则 `R` 会被 parseTypeRef 当成返回类型名)。

- [ ] **Step 5: commit**

```bash
git add internal/javaparser/decls.go internal/javaparser/parser_test.go
git commit -m "feat: javaparser enum body / record header / annotation declaration"
```

---

## Task 10:Golden test — 真实 facade fixture + AST JSON dump

**Files:**
- Create: `internal/javaparser/testdata/parser/facade_v2.java`
- Create: `internal/javaparser/testdata/parser/facade_v2.ast.json`(GO_GENERATE 阶段生成)
- Modify: `internal/javaparser/parser_test.go`

- [ ] **Step 1: 创建 `testdata/parser/facade_v2.java`**

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
@Deprecated
public interface AssetFacade<T extends Number> {
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

    <K> Map<K, List<T>> wrap(K key);

    enum Tier {
        BRONZE, SILVER, GOLD;
    }

    record Page<U>(int offset, int limit, List<U> items) {}

    @interface Cached {
        int ttl() default 60;
    }

    class Default {
        private final String name = "x";
        public String name() { return name; }
    }
}
```

- [ ] **Step 2: 加 generator + golden compare test**

```go
func TestGenerateParserFacadeGolden(t *testing.T) {
	if os.Getenv("GO_GENERATE") != "1" {
		t.Skip("set GO_GENERATE=1 to (re)generate testdata/parser/facade_v2.ast.json")
	}
	src, err := os.ReadFile("testdata/parser/facade_v2.java")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cu, err := Parse(src, "facade_v2.java")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body, _ := json.MarshalIndent(cu, "", "  ")
	if err := os.WriteFile("testdata/parser/facade_v2.ast.json", body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestParseFacadeMatchesGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/parser/facade_v2.java")
	if err != nil {
		t.Fatalf("read src: %v", err)
	}
	want, err := os.ReadFile("testdata/parser/facade_v2.ast.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	cu, err := Parse(src, "facade_v2.java")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, _ := json.MarshalIndent(cu, "", "  ")
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("golden mismatch.\nGOT:\n%s\n\nWANT:\n%s\n", got, want)
	}
}
```

需要 `parser_test.go` 顶部加 import:`"bytes"` / `"encoding/json"` / `"os"`(如果还没有)。

- [ ] **Step 3: 跑 generator 一次**

Run: `GO_GENERATE=1 go test ./internal/javaparser/ -run TestGenerateParserFacadeGolden -v`

Expected: PASS,`testdata/parser/facade_v2.ast.json` 生成。

打开 JSON **人工审查**,重点看:
- 顶层 `Package.Name == "com.acme.facade"`,6 条 imports(含 wildcard 与 static)
- `Types[0].Name == "AssetFacade"`、`Kind == "interface"`、`TypeParams[0].Name == "T"`,Bounds 含 `Number`
- `Methods[0].Name == "queryAssets"`,`ReturnType.String()`(隐含)体现 `QueryResponse<List<Asset>>`,3 个 params,第一个 param 有 `NotNull` annotation
- `Methods[2].TypeParams[0].Name == "K"`(generic method)
- `NestedTypes` 含 4 个:Tier(enum)、Page(record,有 1 个 TypeParam U + 3 个 RecordComponents)、Cached(annotation)、Default(class,1 field + 1 method)

- [ ] **Step 4: 跑 golden compare test**

Run: `go test ./internal/javaparser/ -run TestParseFacadeMatchesGolden -v`

Expected: PASS。

如果 fail:**不要覆盖 golden**,先回看 Step 3 是否生成正确。 任何 lexer/parser 行为变化都应该回到对应 Task 修,不在 golden 上短路。

- [ ] **Step 5: commit**

```bash
git add internal/javaparser/testdata/parser/ internal/javaparser/parser_test.go
git commit -m "feat: javaparser 加 facade golden AST 测试"
```

---

## Task 11:Coverage + vet + build + Out of Scope 文档化

**Files:** all created above + plan 文档

- [ ] **Step 1: 跑 coverage**

Run: `go test ./internal/javaparser/ -cover -v`

Expected: coverage ≥ 90%。 比 C.1 lexer(97%)略低可接受,因为 parser 分支多。 如果某些分支没覆盖到,补 test。 常见漏:
- `parseError` 各路径(可故意构造 malformed 输入触发)
- `non-sealed` 拼接逻辑
- annotation 在参数位置
- record compact ctor skip
- enum trailing comma
- method declared type params + array return type combo

- [ ] **Step 2: vet + build**

Run: `go vet ./internal/javaparser/ && go build ./...`

Expected: 无 warning / error。 注意 `go build ./...` 跑全项目 build,确保 javaparser 包 export 的符号(`Parse / CompilationUnit / *Decl / TypeRef / ...`)不跟现有 schema 包冲突 —— 它们在不同 package,应该天然隔离。

- [ ] **Step 3: 跑全套 regression**

Run: `go test ./...`

Expected: 全 PASS。 schema / app / direct / mcp 等包不受 C.2 影响(没接入)。

- [ ] **Step 4: 看 git status**

Run: `git status && git diff --stat`

预期最终新增 / 修改文件:
```
new file:   internal/javaparser/ast.go
new file:   internal/javaparser/cursor.go
new file:   internal/javaparser/parser.go
new file:   internal/javaparser/typeref.go
new file:   internal/javaparser/decls.go
modified:   internal/javaparser/parser_test.go    (新增 test)
new file:   internal/javaparser/testdata/parser/facade_v2.java
new file:   internal/javaparser/testdata/parser/facade_v2.ast.json
new file:   docs/plans/2026-05-27-java-declaration-parser-c2-ast.md
```

**不能动**:`internal/schema/` 或 `internal/app/` 或 `internal/direct/` 或任何下游 caller —— C.2 是 self-contained AST,不接入现有路径。 接入面是 C.3 的事。

- [ ] **Step 5: 最终 commit**

注意:Task 1-9 已经 frequent commits,Task 10 也 commit。 这里只 commit 文档变化(如果 self-review 过程中修过 plan 文档)+ 任何 last-minute 修补。

```bash
git status
# 如果只剩 plan 文档,commit 它;否则 amend 上一条
git add docs/plans/2026-05-27-java-declaration-parser-c2-ast.md
git commit -m "docs: 加 Java declaration parser C.2 AST plan"
```

---

## Verification

完成全部 Task 后:

```bash
go test ./internal/javaparser/ -v        # 全 PASS
go test ./internal/javaparser/ -cover    # coverage ≥ 90%
go vet ./internal/javaparser/            # 无 warning
go test ./...                            # 整项目无 regression
go build ./...                           # 无 error
```

完成标志:
- `internal/javaparser.Parse(src, path) (*CompilationUnit, error)` 可用,产出完整 AST
- AST 顶点 `CompilationUnit` 含 Package / Imports / Types(递归 NestedTypes)
- `TypeRef.String()` 可还原回 Java 源码字符串,供 C.3 adapter 灌回 `schema.Method.Parameters[i].Type`
- `TypeDecl.TypeParams` / `MethodDecl.TypeParams` 保留 declared type parameters,根治 [[rpc-types-generic-preservation]] P3 edge case 的前提条件已就绪
- 现有 schema / app / direct / mcp 测试全绿,确认 C.2 未接入现有路径

## Out of Scope(留作 follow-up / C.3 + C.4)

- **Full Java Unicode identifier**:沿用 C.1 lexer ASCII 偏置,跟现有 schema regex parser 兼容。 真业务遇到再扩。
- **Modifier mutual exclusion 校验**(`public private` / `static abstract` 同时出现):parser 不挡,treat modifiers as raw token sequence。 spec 校验是另一回事。
- **Annotation argument 语义**:只识别 marker 名,argument 列表 brace-balanced skip。 C.3 adapter 用不到。
- **Method body / expression / lambda / statement**:scope charter 明确不解析。
- **TypeRef.String() round-trip 严格性**:`Map<String, List<X>>` → 拼回 `Map<String, List<X>>`(空格按 ", " 标准化)。 跟原源码 byte-equal 不保证,但 schema.Method.Parameters[i].Type 字符串语义等价(eraseGeneric / extractGenericArgs 都不依赖空格)。
- **Annotation `default` 复杂表达式**:Task 7 把 `default <expr>;` 当一段 skip(直到 `;`),不存 default value。 C.3 adapter 不用 default value。
- **Generic method type parameter shadowing(P3 edge case 根治)**:本 plan 已存 `MethodDecl.TypeParams`,但实际**根治**是在 C.3 adapter 里把 `isLikelyTypeVariable` 启发式换成"精确匹配 declared type params"。 那一步留给 C.3。
- **Pattern matching for instanceof / switch**:不在 declaration 层。
- **Module declaration**(`module-info.java`):本 plan 不覆盖,真用到再扩(加一个 `ModuleDecl` AST 节点 + module 关键字识别)。
- **Generic-qualified inner type `Outer<T>.Inner<U>`**(codex review #3):TypeRef 的单段 `Name` + flat `Args` 表达不支持外段带 generic 之后再 `.Inner<U>`。 在 `Outer<T>` 解析完后停止,留下 `.Inner` 给上层。 真业务 facade 几乎不出现(`Map.Entry<K,V>` 这种 outer 不带 generic 的形态正常 work)。 真撞到时升级 TypeRef 为 segmented representation。
- **Segmented TypeRef representation**(codex review #11):当前 `TypeRef.Name` 是单段 dotted string(如 `"java.util.Map"`)。 C.3 adapter 需要做 import resolution 时,要靠 string 分段 + 现有 schema 包 `resolveBaseType` 处理,稍 awkward 但可工作。 若 C.3 实施时痛点明显,在那一份 plan 里重构成 `TypeRef.Segments []TypeRefSegment{Name, Args}` 形态;C.2 不预 refactor。
- **Annotation method `default` 值的深 skip**(codex review #14):`finishMethodDecl` 用 `skipUntil(TokenSemicolon)` 跳 `default <expr>;`,不做 `()` `{}` 平衡。 因 C.1 lexer 把 string/char/text-block 当原子 token,真实 annotation default 值里出现的 `;` 都已被 lexer 包在 literal 里;`default` 后到 `;` 之间剩余的 `;` 都是结构性分隔符。 codex 二审标 acceptable for declaration-only 用途,不升级。

## What Comes Next(C.3 预告)

C.3 plan(`docs/plans/<date>-java-declaration-parser-c3-adapter.md`)要做:

1. **Adapter 模块**:`internal/schema/adapter_javaparser.go` 新文件,实现 `javaparserToSchema(cu *javaparser.CompilationUnit) ([]schema.Method, map[string]schema.TypeSchema)`
   - `cu.Package + "." + TypeDecl.Name` → `service` / `fqn`
   - `MethodDecl.ReturnType.String()` → `Method.ReturnType`(字符串塞回,带泛型)
   - `MethodDecl.Params[i].Type.String()` + `Params[i].Name` → `Method.Parameters[i]`
   - `cu.Imports` → 现有 `map[shortName]fqn` 形态(wildcard 单独处理)
   - `TypeDecl.Fields[i]` → `TypeSchema.Field`
   - `EnumValues[i].Name` → `TypeSchema.EnumValues[i]`
   - `RecordComponents` → `TypeSchema.Field`(record 视为 class)
   - `NestedTypes` 递归
2. **Wildcard import 解析**:现有 `parseImports` regex 不认 wildcard;adapter 改成把 `import a.b.*` 当作 "pkg fallback 来源",resolve 时按需扫描 `idx.Types` 中前缀匹配的 type
3. **接入面**:替换 `internal/schema/schema.go::parseJavaFile` 的实现 —— 内部改成 `javaparser.Parse(bodyBytes, path)` + 上面的 adapter;`BuildIndex / Search / Describe` 对外签名不动
4. **Golden test 替换**:跑现有 `parser_golden_test.go`,期望大部分 case 仍 PASS;differences 应该集中在:
   - 现有 regex parser 漏识别的 case(如 nested type、wildcard import 解析)→ 新 parser 解析正确 → 接受 golden 更新
   - 新 parser 不一致的 case → 回 C.3 adapter 修
5. **Plan B P3 根治**:`internal/app/rpc_types.go::resolveBaseType` 的 `isLikelyTypeVariable` 启发式,改成精确匹配 method/type 的 declared type params(adapter 把 declared type params 也塞进新加的 `Method.TypeParams` 与 `TypeSchema.TypeParams`)
6. **删除旧 regex 实现**:`packageRE / importRE / typeKindRE / methodRE / fieldRE / enumValueRE` 全部删除,以及 `parseJavaFile / parseMethods / parseTypes / parseFields / parseImports / parseRecordFields / parseEnumValues` 旧函数体
7. **新增 3 个 wildcard / static / inner class golden case**

C.3 工作量预估:1-1.5h plan + 2-3h subagent 执行 + 1 轮 codex consult。

