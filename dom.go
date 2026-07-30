package main

import (
	"bytes"
	_ "embed"
	"gofb/style"
	"gofb/utils"
	"iter"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

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
		styles := style.Styles{}

		// 从样式表更新
		for d := range declarations(matches) {
			switch d.Name {
			case `color`:
				styles.Color = style.StringValue(d.Value)
			case `background-color`:
				styles.BackgroundColor = style.StringValue(d.Value)
			}
		}

		// 从内联覆盖
		inlines := node.Base().inlineStyles
		if inlines.Color.Type != style.VTNone {
			styles.Color = inlines.Color
		}
		if inlines.BackgroundColor.Type != style.VTNone {
			styles.BackgroundColor = inlines.BackgroundColor
		}

		// 从父母继承
		inherit := func(found func(styles style.Styles) style.Value) style.Value {
			for parent := node.Base().Parent; parent != nil; parent = parent.Base().Parent {
				if value := found(parent.Base().computedStyles); value.Type != style.VTNone {
					return value
				}
			}
			return style.Value{}
		}
		if styles.Color.Type == style.VTNone {
			styles.Color = inherit(func(styles style.Styles) style.Value { return styles.Color })
		}
		if styles.BackgroundColor.Type == style.VTNone {
			styles.BackgroundColor = inherit(func(styles style.Styles) style.Value { return styles.BackgroundColor })
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

func loadBox(data []byte) Box {
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
	return transformNodeTree(nodes[0])
}

func transformNodeTree(node *html.Node) Box {
	return transformNode(node)
}

func transformNode(node *html.Node) Box {
	switch node.Type {
	case html.ElementNode:
		return transformELementNode(node)
	case html.TextNode:
		trimmed := strings.TrimSpace(node.Data)
		if trimmed == `` {
			return nil
		}
		t := NewText()
		t.Data = trimmed
		return t
	case html.CommentNode:
		return nil
	default:
		panic(`不认识节点类型`)
	}
}

func transformELementNode(node *html.Node) Box {
	switch node.Data {
	case `block`:
		box := NewBlock()
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		for c := range node.ChildNodes() {
			if b := transformNode(c); b != nil {
				box.appendChild(box, b)
			}
		}
		return box
	case `inline`:
		box := NewInline()
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		for c := range node.ChildNodes() {
			if b := transformNode(c); b != nil {
				box.appendChild(box, b)
			}
		}
		return box
	case `space`:
		box := NewSpacer()
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		return box
	}
	switch node.DataAtom {
	case atom.Button:
		box := NewButton()
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		for c := range node.ChildNodes() {
			if b := transformNode(c); b != nil {
				box.appendChild(box, b)
			}
		}
		return box
	case atom.Img:
		box := NewImage()
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		return box
	}

	panic(`不认识的盒子`)
}
