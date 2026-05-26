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
