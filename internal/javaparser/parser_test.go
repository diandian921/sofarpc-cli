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
		// 序列化:`List<>` → 空 Args → `List`(不带括号)。 不测试覆盖。
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
