package main

import (
	"bytes"
	_ "embed"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

//go:embed main.html
var _main []byte

func loadBox() Box {
	nodes := Must1(html.ParseFragment(bytes.NewReader(_main), &html.Node{
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
		return &Text{Data: trimmed}
	default:
		panic(`不认识节点类型`)
	}
}

func transformELementNode(node *html.Node) Box {
	switch node.DataAtom {
	case atom.Div:
		box := Block{}
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		for c := range node.ChildNodes() {
			if b := transformNode(c); b != nil {
				box.appendChild(b)
			}
		}
		return &box
	case atom.Button:
		box := Button{}
		for _, a := range node.Attr {
			box.ApplyAttributes(a.Key, a.Val)
		}
		for c := range node.ChildNodes() {
			if b := transformNode(c); b != nil {
				if t, ok := b.(*Text); ok {
					t.parent = &box
				}
				box.appendChild(b)
			}
		}
		return &box
	}

	switch node.Data {
	// case `text`:
	// 	box := Text{}
	// 	for _, a := range node.Attr {
	// 		box.ApplyAttributes(a.Key, a.Val)
	// 	}
	// 	if t := node.FirstChild; t != nil {
	// 		if t.Type == html.TextNode {
	// 			box.Data = strings.TrimSpace(t.Data)
	// 			if t.NextSibling != nil {
	// 				panic(`文本不能再有子节点。`)
	// 			}
	// 		} else {
	// 			panic(`不支持的文本子节点`)
	// 		}
	// 	}
	// 	return &box
	default:
		panic(`不认识的盒子`)
	}
}
