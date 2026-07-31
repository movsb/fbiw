package main

import (
	"bytes"
	_ "embed"
	"gofb/style"
	"gofb/utils"
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
	defaultStyles style.Styles
}

func NewDocument(fontManager *FontManager) *Document {
	return &Document{
		fontManager: fontManager,
		defaultStyles: style.Styles{
			BackgroundColor: style.ColorValueFromString(`white`),
			Color:           style.ColorValueFromString(`black`),
			FontFamily:      style.StringValue(`system`),
			FontSize:        style.NumberValue(25),
		},
	}
}

//go:embed main.html
var _main []byte

//go:embed style.css
var _styles []byte

func applyStyles(root Box, sheets []*style.Sheet) {
	nodeMatch := func(node Box, selector style.Selector) (uint32, bool) {
		specificity := uint32(0)
		for _, selector := range slices.Backward(selector) {
			match := false
			if selector.Tag == node.Base().Tag {
				match = true
				specificity += 1 << 0
			} else if node.Base().Class.ContainsAny(selector.Class...) {
				match = true
				specificity += 1 << 8
			} else if selector.ID != `` && selector.ID == node.Base().ID {
				match = true
				specificity += 1 << 16
			}
			if !match {
				return 0, false
			}
		}
		return specificity, true
	}

	findMatches := func(node Box) []style.RuleMatch {
		matches := []style.RuleMatch{}
		for _, sheet := range sheets {
			for _, rule := range sheet.Rules {
				if spec, ok := nodeMatch(node, rule.Selector); ok {
					matches = append(matches, style.RuleMatch{
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

	compute := func(matches []style.RuleMatch, node Box) style.Styles {
		declarations := func(matches []style.RuleMatch) iter.Seq[style.Declaration] {
			// 按相关性递增排序（后来居上）。
			slices.SortFunc(matches, func(a, b style.RuleMatch) int {
				return int(a.Specificity) - int(b.Specificity)
			})
			return func(yield func(style.Declaration) bool) {
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
		for field, value := range stylesValue.Elem().Fields() {
			if !style.ShouldInherit(strings.ToLower(field.Name)) {
				continue
			}
			if field.Type == reflect.TypeFor[style.Value]() {
				for parent := node.Base().Parent; parent != nil; parent = parent.Base().Parent {
					parentValue := reflect.ValueOf(&parent.Base().computedStyles)
					parentField := parentValue.Elem().FieldByIndex(field.Index)
					value3 := parentField.Interface().(style.Value)
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
			if field.Type == reflect.TypeFor[style.Value]() {
				value2 := value.Interface().(style.Value)
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

func loadStyles() *style.Sheet {
	return utils.Must1(style.ParseStyle(_styles))
}

func loadBox(doc *Document, data []byte) Box {
	nodes := utils.Must1(html.ParseFragment(bytes.NewReader(data), &html.Node{
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
			utils.Must(box.ApplyAttributes(a.Key, a.Val))
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
			utils.Must(box.ApplyAttributes(a.Key, a.Val))
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
