package javaparser

import (
	"fmt"
	"strings"
)

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

// readNumber 识别 int / long / float / double 字面量。
// 支持 decimal / hex (0x) / binary (0b) / octal (0...),后缀 L/l/F/f/D/d。
// 接收 Java 7+ 下划线分隔(如 1_000_000)。
// 关键 1:'+'/'-' 必须紧跟 exponent 标志(e/E/p/P)才属于 number 的一部分,
//   否则会把 "1-2" 错误吃成一个 token。
// 关键 2:a-f 字符只在 hex 模式(0x / 0X 开头)接受,否则 "3.14f" 的 'f'
//   会被当 hex digit 吃掉,suffix switch 永远进不去。
func (l *lexer) readNumber(line, col, off int) Token {
	start := l.pos
	l.advance() // 首字符 0-9 已校验
	hexMode := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
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
		// 进入 hex 前缀
		if (c == 'x' || c == 'X') && l.pos-start == 1 && l.src[start] == '0' {
			hexMode = true
			l.advance()
			continue
		}
		// 进入 binary 前缀
		if (c == 'b' || c == 'B') && l.pos-start == 1 && l.src[start] == '0' {
			l.advance()
			continue
		}
		if (c >= '0' && c <= '9') || c == '_' || c == '.' {
			l.advance()
			continue
		}
		// hex digits a-f 仅在 hex 模式接受
		if hexMode && ((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			l.advance()
			continue
		}
		// decimal exponent e/E 通用;hex float exponent p/P 仅 hex 模式
		if c == 'e' || c == 'E' {
			l.advance()
			continue
		}
		if hexMode && (c == 'p' || c == 'P') {
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
