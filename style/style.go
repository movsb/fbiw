package style

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

type Rule struct {
	// 只表示单条选择器（类似逗号分隔的会被拆成多条）
	Selector     Selector
	Declarations []Declaration
}

type _Rule struct {
	Selectors    []Selector
	Declarations []Declaration
}

type Selector = []NodeSelector

type NodeSelector struct {
	Tag   string
	Class []string
	ID    string

	// 可直接比较大小，但不等于真实的css相关性，需要转换。
	// 8 + 8 + 8   +  8
	// 0   id  class  tag
	Specificity uint32
}

type Declaration struct {
	Name  string
	Value string
}

type Sheet struct {
	Rules []Rule
}

func ParseStyle(data []byte) (_ *Sheet, outErr error) {
	buf := &BufioReader{
		Reader: bufio.NewReader(bytes.NewReader(data)),
	}

	defer func() {
		if e := recover(); e != nil {
			outErr = fmt.Errorf(`%v`, e)
		}
	}()

	ss := Sheet{}
	for {
		buf.skipSpaces()
		if buf.peekByte() == 0 {
			break
		}
		rule := parseRule(buf)
		for _, s := range rule.Selectors {
			ss.Rules = append(ss.Rules, Rule{
				Selector:     s,
				Declarations: rule.Declarations,
			})
		}
	}

	return &ss, nil
}

func parseRule(buf *BufioReader) _Rule {
	r := _Rule{}

	for {
		selectors := parseSelector(buf)
		r.Selectors = append(r.Selectors, selectors)
		buf.skipSpaces()
		b := buf.peekByte()
		if b == ',' {
			continue
		}
		break
	}

	buf.skipSpaces()
	if b := buf.peekByte(); b != '{' {
		panic(`缺少 {`)
	}
	buf.Discard(1)

	buf.skipSpaces()
	if b := buf.peekByte(); b != '}' {
		r.Declarations = parseDeclarations(buf)
	}

	buf.skipSpaces()
	if b := buf.peekByte(); b != '}' {
		panic(`缺少 }`)
	}
	buf.Discard(1)

	return r
}

type BufioReader struct {
	*bufio.Reader
}

func (b *BufioReader) skipSpaces() {
	for {
		c, err := b.Peek(1)
		if err != nil {
			if err == io.EOF {
				return
			}
			panic(err)
		}
		switch c[0] {
		case ' ', '\t', '\n':
			b.Discard(1)
			continue
		default:
			return
		}
	}
}

func (b *BufioReader) peekByte() byte {
	c, err := b.Reader.Peek(1)
	if err != nil {
		return 0
	}
	return c[0]
}

// 解析选择器。
//
// 支持的语法：
//   - block
//   - #id
//   - .class
//   - block inline
func parseSelector(buf *BufioReader) []NodeSelector {
	selectors := []NodeSelector{}
	current := NodeSelector{}

	for {
		buf.skipSpaces()

		b := buf.peekByte()
		if b == '#' {
			buf.Discard(1)
			current.ID = parseIdent(buf)
			current.Specificity += 1 << 16
		} else if b == '.' {
			buf.Discard(1)
			current.Class = append(current.Class, parseIdent(buf))
			current.Specificity += 1 << 8
		} else if isIdentChar(b) {
			current.Tag = parseIdent(buf)
			current.Specificity += 1 << 0
		} else if b == ',' || b == '{' || b == 0 {
			break
		} else {
			panic(`不认识的字符`)
		}
		selectors = append(selectors, current)
		current = NodeSelector{}
	}

	if len(selectors) <= 0 {
		panic(`没有选择器。`)
	}

	return selectors
}

func parseDeclarations(buf *BufioReader) []Declaration {
	d := []Declaration{}

	current := Declaration{}

	for {
		buf.skipSpaces()
		current.Name = parseIdent(buf)
		buf.skipSpaces()
		if b := buf.peekByte(); b != ':' {
			panic(`缺少 :`)
		}
		buf.Discard(1)
		tmp := []byte{}
		for {
			b := buf.peekByte()
			if b == ';' || b == '0' {
				break
			}
			buf.Discard(1)
			tmp = append(tmp, b)
		}
		if len(tmp) <= 0 {
			panic(`没有值`)
		}
		current.Value = strings.TrimSpace(string(tmp))
		d = append(d, current)
		if b := buf.peekByte(); b != ';' {
			panic(`缺少 ;`)
		}
		buf.Discard(1)
		buf.skipSpaces()
		if b := buf.peekByte(); b == 0 || b == '}' {
			break
		}
	}

	return d
}

func isIdentChar(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || b == '-' || b == '_'
}

func parseIdent(buf *BufioReader) string {
	tmp := []byte{}
	for {
		b := buf.peekByte()
		if isIdentChar(b) {
			buf.Discard(1)
			tmp = append(tmp, b)
			continue
		}
		break
	}
	if len(tmp) == 0 {
		panic(`缺少标识符`)
	}
	return string(tmp)
}
