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
	// 必须在消费 public 之前调用 —— 此时 idx 已被 skipTrivia 推到 public,
	// idx-1 是 Javadoc。 若先 consume(public),idx 推到 class,回看遇到 public(非 trivia)
	// 立即返回 ""。
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
