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
