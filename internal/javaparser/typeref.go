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
