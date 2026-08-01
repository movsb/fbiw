package main

import (
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log"
	"path"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Document struct {
	// 资源包
	fsys fs.FS
	// HTML内引用的所有资源文件基于此目录。
	skinDir string

	fontManager  *FontManager
	imageManager *ImageManager

	// 默认样式总是用于初始化拷贝，所以不需要用指针，方便拷贝并覆盖。
	defaultStyles Styles

	// 文档内 <style> 元素提供的样式
	styleSheet *Sheet

	// 文档body根节点
	root Box

	width, height int
}

func NewDocument(
	fileSystem fs.FS,
	fontManager *FontManager,
	imageManager *ImageManager,
) *Document {
	doc := &Document{
		fsys:         fileSystem,
		fontManager:  fontManager,
		imageManager: imageManager,
		defaultStyles: Styles{
			Color:      ColorValueFromString(`black`),
			FontFamily: StringValue(`system`),
			FontSize:   NumberValue(25),
		},
	}
	return doc
}

// 从指定文件加载内容。
//
// 文件来源于初始化时的文件系统。
func (doc *Document) Load(name string, skinDir string) error {
	fp, err := doc.fsys.Open(name)
	if err != nil {
		return err
	}
	defer fp.Close()
	if err := doc.parse(fp); err != nil {
		return fmt.Errorf(`文档解析失败：%w`, err)
	}
	if skinDir == `` {
		skinDir = `.`
	}
	doc.skinDir = skinDir
	doc.SetDirty()
	return nil
}

// html parser 的问题：
//
//   - <spacer/> 写法不支持。html5 没有自闭合标签，会被解析成 <spacer>（“/”直接没了），
//     但是如果后面有<inline>这种，则会被解析成 <spacer><inline></inline></spacer>...，错得离谱。
//
// 使用 html.Tokenizer 是一个好做法，支持解析 <style> 作为 raw text node（xml不能出现 <）。
// 重写需要一定时间，先暂时用 html parser。
func (doc *Document) parse(content io.Reader) error {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: `div`}
	nodes, err := html.ParseFragment(content, context)
	if err != nil {
		return fmt.Errorf(`文档解析失败：%w`, err)
	}
	// 去掉根节点所有的文本节点，确保只有一个节点。
	nodes = slices.DeleteFunc(nodes, func(node *html.Node) bool {
		return node.Type != html.ElementNode
	})
	if len(nodes) != 1 {
		return fmt.Errorf(`找不到文档的元素节点`)
	}

	first := nodes[0]
	if first.Data != `document` {
		return fmt.Errorf(`根节点需要是 <document>`)
	}

	var (
		styleNode *html.Node
		bodyNode  *html.Node
	)
	for child := range first.ChildNodes() {
		switch child.Type {
		case html.ElementNode:
			if child.DataAtom == atom.Style {
				if styleNode != nil {
					return fmt.Errorf(`重复的样式节点`)
				}
				styleNode = child
			} else if child.Data == `block` {
				if bodyNode != nil {
					return fmt.Errorf(`根元素下重复的block节点`)
				}
				bodyNode = child
			} else {
				return fmt.Errorf(`根元素下不认识的节点：%s`, child.Data)
			}
		case html.TextNode:
			if strings.TrimSpace(child.Data) != `` {
				return fmt.Errorf(`根元素下不能有文本内容`)
			}
		case html.CommentNode:
		}
	}

	if styleNode != nil {
		if styleNode.FirstChild == nil {
			doc.styleSheet = &Sheet{}
		} else if styleNode.FirstChild.NextSibling != nil {
			return fmt.Errorf(`样式格式错误`)
		} else if styleNode.FirstChild.Type != html.TextNode {
			return fmt.Errorf(`不是文本节点`)
		} else {
			textData := styleNode.FirstChild.Data
			sheet, err := ParseStyle([]byte(textData))
			if err != nil {
				return fmt.Errorf(`样式解析失败：%w`, err)
			}
			doc.styleSheet = sheet
		}
	}

	if bodyNode == nil {
		return fmt.Errorf(`缺少block节点`)
	}

	box, err := transformFromHTMLNodes(doc, bodyNode)
	if err != nil {
		return fmt.Errorf(`文档内容节点解析失败：%w`, err)
	}

	doc.root = box

	return nil
}

// 获取指定ID的元素。
func (doc *Document) GetElementByID(id string) Box {
	var out Box
	doc.walkRoot(func(box Box) bool {
		if box.Base().ID == id {
			out = box
			return false
		}
		return true
	})
	return out
}

func transformFromHTMLNodes(doc *Document, node *html.Node) (Box, error) {
	processNode := func(box Box, node *html.Node, voidElement bool) (Box, error) {
		for _, a := range node.Attr {
			// 所有的节点理应都是从BaseBox继承的，所以接口不可能为空。
			// 但是也不能调BaseBox().Apply...，因为子类有方法覆盖。
			if err := box.(AttributeApplier).ApplyAttributes(a.Key, a.Val); err != nil {
				return nil, err
			}
		}
		if voidElement && node.FirstChild != nil {
			return nil, fmt.Errorf(`节点不能包含子节点：%s`, node.Data)
		}
		for c := range node.ChildNodes() {
			child, err := transformFromHTMLNodes(doc, c)
			if err != nil {
				return nil, err
			}
			// 只有空白的文本节点暂时返回空。
			if child == nil {
				continue
			}
			box.Base().appendChild(box, child)
		}
		return box, nil
	}
	switch node.Type {
	case html.ElementNode:
		switch node.Data {
		case `block`:
			return processNode(NewBlock(doc), node, false)
		case `inline`:
			return processNode(NewInline(doc), node, false)
		case `spacer`:
			return processNode(NewSpacer(doc), node, true)
		case `button`:
			return processNode(NewButton(doc), node, false)
		case `img`:
			return processNode(NewImage(doc), node, true)
		case `space`:
			return processNode(NewSpacer(doc), node, true)
		}
	case html.TextNode:
		trimmed := strings.TrimSpace(node.Data)
		if trimmed == `` {
			return nil, nil
		}
		t := NewText(doc)
		t.Data = trimmed
		return t, nil
	}
	return nil, fmt.Errorf(`未识别的标签：%v`, node.Data)
}

// 手动弄脏。
func (doc *Document) SetDirty() {
	doc.root.Base().Dirty = true
}
func (doc *Document) IsDirty() bool {
	return doc.root.Base().IsDirty()
}

func (doc *Document) Resize(width, height int) {
	doc.width, doc.height = width, height
}

// 对每个节点计算样式。
func (doc *Document) Style() error {
	styler := _Styler{doc: doc}
	return styler.Style(doc.styleSheet)
}

func (doc *Document) StyleNoError() {
	if err := doc.Style(); err != nil {
		log.Println(err)
	}
}

// 重新布局整个文档。
//
// TODO 把计算方式从元素自身拆解到这里来。
func (doc *Document) Layout() {
	doc.root.Calc(doc.width, doc.height)
}

// 绘制文档。
func (doc *Document) Paint(canvas *Canvas) {
	doc.root.Draw(canvas)
}

func (doc *Document) walkRoot(callback func(box Box) bool) {
	var walk func(box Box, callback func(Box) bool) bool
	walk = func(box Box, callback func(Box) bool) bool {
		if !callback(box) {
			return false
		}
		for _, child := range box.Base().Children {
			if !walk(child, callback) {
				return false
			}
		}
		return true
	}
	walk(doc.root, callback)
}

func (doc *Document) loadImage(src string) (DecodedImage, error) {
	return doc.imageManager.GetImageCached(doc.fsys, path.Join(doc.skinDir, src))
}

// 这个类的存在只是不想document下有太多不相关的方法。
type _Styler struct {
	doc *Document
}

func (s _Styler) Style(sheet *Sheet) (outErr error) {
	s.doc.walkRoot(func(box Box) bool {
		rules := s.findRulesFor(box, sheet)
		if err := s.computeStyles(box, rules); err != nil {
			outErr = fmt.Errorf(`样式应用失败：%w`, err)
			return false
		}
		return true
	})
	return
}

// 从样式规则里面找出匹配节点的规则集。
// 找到的规则没有排序。
func (s _Styler) findRulesFor(node Box, sheet *Sheet) []RuleMatch {
	matches := []RuleMatch{}
	for _, rule := range sheet.Rules {
		if s.match(node, rule.Selector) {
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
	return matches
}

// 为节点计算样式。
// 计算后直接保存到节点。
func (s _Styler) computeStyles(node Box, rules []RuleMatch) error {
	declarations := func(rules []RuleMatch) iter.Seq[Declaration] {
		// 按相关性递增排序（后来居上）。
		slices.SortFunc(rules, func(a, b RuleMatch) int {
			return int(a.Specificity) - int(b.Specificity)
		})
		return func(yield func(Declaration) bool) {
			for _, rule := range rules {
				for _, d := range rule.Declarations {
					if !yield(d) {
						return
					}
				}
			}
		}
	}

	// 从全局拷贝默认
	styles := s.doc.defaultStyles

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
	for d := range declarations(rules) {
		if err := styles.Set(d.Name, d.Value); err != nil {
			return fmt.Errorf(`样式应用错误：%w`, err)
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

	// 直接保存起来。
	node.Base().computedStyles = styles

	return nil
}

// 判断节点是否和选择器完整匹配。
func (s _Styler) match(node Box, selector Selector) bool {
	// 先是自身匹配
	if !s._matchSelf(node, selector[len(selector)-1]) {
		return false
	}

	if len(selector) <= 1 {
		return true
	}

	// 如果自身匹配，继续往上寻找可能的祖先匹配。
	// 每一个后代选择器都需要对每个祖先进行尝试。
	ancestorSelectors := selector[:len(selector)-1]

	return s._matchAncestors(node, ancestorSelectors)
}

// 判断单个简单选择器是否匹配当前节点。
func (s _Styler) _matchSelf(node Box, selector NodeSelector) bool {
	return (selector.Tag == `` || selector.Tag == node.Base().Tag) &&
		(len(selector.Class) == 0 || node.Base().Class.ContainsAll(selector.Class...)) &&
		(selector.ID == `` || selector.ID == node.Base().ID)
}

func (s _Styler) _matchAncestors(node Box, ancestorSelectors Selector) bool {
	return s._matchAncestorsRecursive(node, ancestorSelectors, len(ancestorSelectors)-1)
}

// 找单个选择器能匹配的至少一个祖先。
// 基本约等于 document.querySelector 的功能。
// 递归好烧脑。
func (s _Styler) _matchAncestorsRecursive(node Box, ancestorSelectors Selector, backIndex int) bool {
	// 前面的所有选择器均匹配上了，并且已经没有选择器了，
	// 所以到这里就表示所有选择器匹配成功了。
	if backIndex < 0 {
		return true
	}
	for ancestor := range node.Base().Ancestors() {
		if s._matchSelf(ancestor, ancestorSelectors[backIndex]) {
			if s._matchAncestorsRecursive(ancestor, ancestorSelectors, backIndex-1) {
				return true
			}
		}
	}
	return false
}
