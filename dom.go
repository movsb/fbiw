package main

import (
	"bytes"
	_ "embed"
	"iter"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Document struct {
	fontManager *FontManager

	// 默认样式总是用于初始化拷贝，所以不需要用指针，方便拷贝并覆盖。
	defaultStyles Styles
}

func NewDocument(fontManager *FontManager) *Document {
	return &Document{
		fontManager: fontManager,
		defaultStyles: Styles{
			Color:      ColorValueFromString(`black`),
			FontFamily: StringValue(`system`),
			FontSize:   NumberValue(25),
		},
	}
}

//go:embed main.html
var _main []byte

//go:embed style.css
var _styles []byte

func applyStyles(root Box, sheets []*Sheet) {
	nodeMatch := func(node Box, selector Selector) bool {
		match := func(node Box, selector NodeSelector) bool {
			return (selector.Tag == `` || selector.Tag == node.Base().Tag) &&
				(len(selector.Class) == 0 || node.Base().Class.ContainsAll(selector.Class...)) &&
				(selector.ID == `` || selector.ID == node.Base().ID)
		}

		// 先是自身匹配
		if !match(node, selector[len(selector)-1]) {
			return false
		}

		if len(selector) <= 1 {
			return true
		}

		// 如果自身匹配，继续往上寻找可能的祖先匹配。
		// 每一个后代选择器都需要对每个祖先进行尝试。
		ancestorSelectors := selector[:len(selector)-1]

		// 找单个选择器能匹配的至少一个祖先。
		// 基本约等于 document.querySelector 的功能。
		// 递归好烧脑。
		var matchAncestors func(node Box, backIndex int) bool
		matchAncestors = func(node Box, backIndex int) bool {
			// 前面的所有选择器均匹配上了，并且已经没有选择器了，
			// 所以到这里就表示所有选择器匹配成功了。
			if backIndex < 0 {
				return true
			}
			for ancestor := range node.Base().Ancestors() {
				if match(ancestor, ancestorSelectors[backIndex]) {
					if matchAncestors(ancestor, backIndex-1) {
						return true
					}
				}
			}
			return false
		}

		return matchAncestors(node, len(ancestorSelectors)-1)
	}

	findMatches := func(node Box) []RuleMatch {
		matches := []RuleMatch{}
		for _, sheet := range sheets {
			for _, rule := range sheet.Rules {
				if nodeMatch(node, rule.Selector) {
					spec := uint32(0)
					for _, sel := range rule.Selector {
						spec += sel.Specificity
					}
					matches = append(matches, RuleMatch{
						Specificity:  spec,
						Declarations: rule.Declarations,
					})
				}
			}
		}
		return matches
	}

	var walkBox func(b Box, h func(b Box))
	walkBox = func(b Box, h func(b Box)) {
		h(b)
		for _, c := range b.Base().Children {
			walkBox(c, h)
		}
	}

	compute := func(matches []RuleMatch, node Box) Styles {
		declarations := func(matches []RuleMatch) iter.Seq[Declaration] {
			// 按相关性递增排序（后来居上）。
			slices.SortFunc(matches, func(a, b RuleMatch) int {
				return int(a.Specificity) - int(b.Specificity)
			})
			return func(yield func(Declaration) bool) {
				for _, rule := range matches {
					for _, d := range rule.Declarations {
						if !yield(d) {
							return
						}
					}
				}
			}
		}

		// 从全局获取默认
		styles := root.Base().Document.defaultStyles

		inlines := &node.Base().inlineStyles
		inlineValue := reflect.ValueOf(inlines)
		stylesValue := reflect.ValueOf(&styles)

		// 从父母继承
		// TODO 优化：如果样式表或内联表有值，则无需再从父母继承。
		for field, value := range stylesValue.Elem().Fields() {
			if !ShouldInherit(field.Name) {
				continue
			}
			if field.Type == reflect.TypeFor[Value]() {
				for parent := node.Base().Parent; parent != nil; parent = parent.Base().Parent {
					parentValue := reflect.ValueOf(&parent.Base().computedStyles)
					parentField := parentValue.Elem().FieldByIndex(field.Index)
					value3 := parentField.Interface().(Value)
					if !value3.Empty() {
						value.Set(parentField)
						// 从最近的祖先那里获取一次即可。
						break
					}
				}
			}
		}

		// 从样式表更新
		for d := range declarations(matches) {
			if err := styles.Set(d.Name, d.Value); err != nil {
				panic(err)
			}
		}

		// 从内联覆盖
		// 如果性能孬就换成独立的复制过程。
		for field, value := range inlineValue.Elem().Fields() {
			if field.Type == reflect.TypeFor[Value]() {
				value2 := value.Interface().(Value)
				if !value2.Empty() {
					dstValue := stylesValue.Elem().FieldByIndex(field.Index)
					dstValue.Set(value)
				}
			}
		}

		return styles
	}

	walkBox(root, func(node Box) {
		matches := findMatches(node)
		styles := compute(matches, node)
		node.Base().computedStyles = styles
	})
}

func loadStyles() *Sheet {
	return Must1(ParseStyle(_styles))
}

func loadBox(doc *Document, data []byte) Box {
	nodes := Must1(html.ParseFragment(bytes.NewReader(data), &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.Div,
		Data:     `div`,
	}))
	if len(nodes) != 1 {
		if len(nodes) == 2 && nodes[1].Type == html.TextNode && strings.TrimSpace(nodes[1].Data) == `` {

		} else {
			panic(`只能有一个节点`)
		}
	}
	// html.Render(os.Stdout, nodes[0])
	return transformNodeTree(doc, nodes[0])
}

func transformNodeTree(doc *Document, node *html.Node) Box {
	return transformNode(doc, node)
}

func transformNode(doc *Document, node *html.Node) Box {
	switch node.Type {
	case html.ElementNode:
		return transformELementNode(doc, node)
	case html.TextNode:
		trimmed := strings.TrimSpace(node.Data)
		if trimmed == `` {
			return nil
		}
		t := NewText(doc)
		t.Data = trimmed
		return t
	case html.CommentNode:
		return nil
	default:
		panic(`不认识节点类型`)
	}
}

func transformELementNode(doc *Document, node *html.Node) Box {
	switch node.Data {
	case `block`:
		box := NewBlock(doc)
		for _, a := range node.Attr {
			Must(box.ApplyAttributes(a.Key, a.Val))
		}
		for c := range node.ChildNodes() {
			if b := transformNode(doc, c); b != nil {
				box.appendChild(box, b)
			}
		}
		return box
	case `inline`:
		box := NewInline(doc)
		for _, a := range node.Attr {
			Must(box.ApplyAttributes(a.Key, a.Val))
		}
		for c := range node.ChildNodes() {
			if b := transformNode(doc, c); b != nil {
				box.appendChild(box, b)
			}
		}
		return box
	case `space`:
		box := NewSpacer(doc)
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		return box
	}
	switch node.DataAtom {
	case atom.Button:
		box := NewButton(doc)
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		for c := range node.ChildNodes() {
			if b := transformNode(doc, c); b != nil {
				box.appendChild(box, b)
			}
		}
		return box
	case atom.Img:
		box := NewImage(doc)
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		return box
	}

	panic(`不认识的盒子`)
}
