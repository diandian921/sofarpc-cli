package javaparser

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestTokenizeEmptyReturnsEOF(t *testing.T) {
	tokens, err := Tokenize([]byte(""))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(tokens) != 1 || tokens[0].Kind != TokenEOF {
		t.Fatalf("tokens = %#v", tokens)
	}
}

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

// codex review (Plan C.1 实施 commit) 抓的两个 hex 边界 P2:
// 1. 0xdeadbeef 里的 'e' 是 hex digit 不是 decimal exponent,不能升级 Double
// 2. 0x1.0p2f 里 p 后的 f 是 float suffix 不再是 hex digit
func TestHexLiteralEdgeCases(t *testing.T) {
	cases := []struct {
		src  string
		kind TokenKind
	}{
		{"0xdeadbeef", TokenIntLiteral},
		{"0xCAFE_BABE", TokenIntLiteral},
		{"0xdeadbeefL", TokenLongLiteral},
		{"0x1.0p2", TokenDoubleLiteral},
		{"0x1.0p2f", TokenFloatLiteral},
		{"0x1.0p+8", TokenDoubleLiteral},
		{"0x1p10F", TokenFloatLiteral},
		{"1e10", TokenDoubleLiteral},
		{"1E5L", TokenLongLiteral}, // decimal int with no '.' and L suffix
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
			if tokens[0].Kind != tc.kind || tokens[0].Value != tc.src {
				t.Errorf("got %v want kind=%v val=%q", tokens[0], tc.kind, tc.src)
			}
		})
	}
}

// codex review 3 抓的 P2:hex digit e/E 紧接 +/- 时,+/- 不应被吞。
// 之前的判断只看"前一字符正好是 e/E/p/P",没区分这个 e 是 hex digit 还是
// decimal exponent。 "0x1e+1" 应该是 3 token (IntLiteral + Other + IntLiteral)。
func TestHexDigitEFollowedBySignBoundary(t *testing.T) {
	cases := []struct {
		src      string
		wantVals []string
		wantKind []TokenKind
	}{
		{"0x1e+1", []string{"0x1e", "+", "1"}, []TokenKind{TokenIntLiteral, TokenOther, TokenIntLiteral}},
		{"0x1E-2", []string{"0x1E", "-", "2"}, []TokenKind{TokenIntLiteral, TokenOther, TokenIntLiteral}},
		// 对照:真 decimal exponent 仍然要把 sign 吞进 number
		{"1e+10", []string{"1e+10"}, []TokenKind{TokenDoubleLiteral}},
		{"1E-5", []string{"1E-5"}, []TokenKind{TokenDoubleLiteral}},
		// 对照:hex float exponent p 之后的 sign 也要吞
		{"0x1.0p+8", []string{"0x1.0p+8"}, []TokenKind{TokenDoubleLiteral}},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tokens, _ := Tokenize([]byte(tc.src))
			// 末尾必有 EOF
			if len(tokens) != len(tc.wantVals)+1 {
				t.Fatalf("tokens = %v", tokens)
			}
			for i, want := range tc.wantVals {
				if tokens[i].Value != want || tokens[i].Kind != tc.wantKind[i] {
					t.Errorf("tokens[%d] = %v, want val=%q kind=%v", i, tokens[i], want, tc.wantKind[i])
				}
			}
		})
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

// TestGenerateFacadeGolden 是 env-gated 生成器:
//   GO_GENERATE=1 go test ./internal/javaparser/ -run TestGenerateFacadeGolden
// 跑完后人工审查 testdata/facade.tokens.json 是否符合预期,审查通过后
// 后续 TestTokenizeFacadeMatchesGolden 用同一份数据做 regression。
// 常规 go test 直接 skip,不会污染 golden。
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

// TestTokenizeFacadeMatchesGolden 比对 testdata/facade.tokens.json。
// 如果 golden 文件不存在(Task 7 step 1 阶段),这个 test 跳过;
// step 2 生成完 golden 后这个 test 自动启用做 regression。
func TestTokenizeFacadeMatchesGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/facade.tokens.json")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("golden not yet generated; run GO_GENERATE=1 first")
		}
		t.Fatalf("read golden: %v", err)
	}
	src, err := os.ReadFile("testdata/facade.java")
	if err != nil {
		t.Fatalf("read src: %v", err)
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

// TestTokenKindString 覆盖所有 TokenKind 的 String() 分支(coverage 拉到 95%+)。
// 这些方法只在 debug print / error message 时被调,常规 tokenize 路径不会触发。
func TestTokenKindString(t *testing.T) {
	allKinds := []TokenKind{
		TokenEOF, TokenError, TokenLineComment, TokenBlockComment, TokenJavadoc,
		TokenIdent, TokenKeyword,
		TokenIntLiteral, TokenLongLiteral, TokenFloatLiteral, TokenDoubleLiteral,
		TokenStringLiteral, TokenCharLiteral, TokenBoolLiteral, TokenNullLiteral,
		TokenLBrace, TokenRBrace, TokenLParen, TokenRParen,
		TokenLBracket, TokenRBracket, TokenLAngle, TokenRAngle,
		TokenComma, TokenSemicolon, TokenDot, TokenAt,
		TokenAssign, TokenQuestion, TokenStar, TokenColon, TokenAmp,
		TokenEllipsis, TokenArrow, TokenOther,
	}
	for _, k := range allKinds {
		if k.String() == "" {
			t.Errorf("TokenKind(%d).String() returned empty", int(k))
		}
	}
	unknown := TokenKind(999)
	if unknown.String() != "Kind(999)" {
		t.Errorf("unknown kind = %q, want Kind(999)", unknown.String())
	}
	tok := Token{Kind: TokenIdent, Value: "foo", Line: 1, Col: 2}
	if tok.String() == "" {
		t.Error("Token.String() returned empty")
	}
}
