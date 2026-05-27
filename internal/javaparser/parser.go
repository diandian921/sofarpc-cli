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
