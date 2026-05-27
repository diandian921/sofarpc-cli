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
	Package    *PackageDecl
	Imports    []ImportDecl
	Types      []TypeDecl
}

// PackageDecl `package a.b.c;`
type PackageDecl struct {
	Name string
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
	TypeKindAnnotation
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
	Modifiers   []string
	Annotations []Annotation
	Javadoc     string
	Name        string
	TypeParams  []TypeParam
	Extends     []TypeRef
	Implements  []TypeRef
	Permits     []TypeRef

	Methods          []MethodDecl
	Fields           []FieldDecl
	EnumValues       []EnumValue
	RecordComponents []ParamDecl
	NestedTypes      []TypeDecl

	Pos Position
}

// TypeParam declared type parameter:`T extends A & B`。
type TypeParam struct {
	Name   string
	Bounds []TypeRef
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
	WildcardBound *TypeRef
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
	TypeParams    []TypeParam
	ReturnType    TypeRef
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

// EnumValue 一个 enum 常量。 ArgsRaw 留空(C.2 不解析 enum constant arguments)。
type EnumValue struct {
	Annotations []Annotation
	Javadoc     string
	Name        string
	Pos         Position
}
