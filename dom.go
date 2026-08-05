package fbiw

import (
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type Document struct {
	// 调试用的。
	name string

	app     *App
	display bool

	// 资源包
	fsys fs.FS
	// HTML内引用的所有资源文件基于此目录。
	skinDir string

	width, height int

	fontManager  *FontManager
	imageManager *ImageManager

	// 默认样式总是用于初始化拷贝，所以不需要用指针，方便拷贝并覆盖。
	defaultStyles Styles

	// 文档内 <style> 元素提供的样式
	styleSheet *Sheet

	// 文档body根节点。
	// parse完成后写入。
	root Box

	// 是否脏了（需要排版或重绘）
	// 修改了影响排版的属性。比如大小、隐藏、删减。
	layoutDirty bool
	// 是否只影响绘制。
	// 如果只影响了绘制，不应该重新排版。
	// 比如只修改了背景色。
	paintDirty bool

	// 事件托管。
	delegator Delegator
}

func _NewDocument(
	width, height int,
	fileSystem fs.FS,
	fontManager *FontManager,
	imageManager *ImageManager,
) *Document {
	doc := &Document{
		width:        width,
		height:       height,
		fsys:         fileSystem,
		fontManager:  fontManager,
		imageManager: imageManager,
	}
	return doc
}

func (doc *Document) Close() {
	if doc.app != nil {
		doc.app._CloseDocument(doc)
	}
}

// 从指定文件加载内容。
//
// 文件来源于初始化时的文件系统。
func (doc *Document) load(name string, skinDir string) error {
	fp, err := doc.fsys.Open(name)
	if err != nil {
		return err
	}
	defer fp.Close()
	if err := doc.parse(fp); err != nil {
		return fmt.Errorf(`文档解析失败：%w`, err)
	}

	// 计算文档默认样式。
	docBox := _DocBox{BaseBox: BaseBox{Tag: `document`}}
	if err := doc.style(&docBox, false); err != nil {
		return err
	}
	doc.defaultStyles = docBox.computedStyles

	// 为所有子元素计算样式。
	if err := doc.style(doc.root, true); err != nil {
		return err
	}

	if skinDir == `` {
		skinDir = `.`
	}
	doc.skinDir = skinDir
	doc.layoutDirty = true
	doc.paintDirty = true
	doc.name = name
	return nil
}

// 只用于默认样式计算。
type _DocBox struct {
	BaseBox
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

	box, err := _NodeTransformer{doc}.Transform(bodyNode)
	if err != nil {
		return fmt.Errorf(`文档内容节点解析失败：%w`, err)
	}

	doc.root = box

	return nil
}

func (doc *Document) dirty() bool {
	return doc.layoutDirty || doc.paintDirty
}
func (doc *Document) clean() {
	doc.layoutDirty = false
	doc.paintDirty = false
}

func (doc *Document) sync(canvas *Canvas) {
	if doc.layoutDirty {
		doc.layout()
		doc.paintDirty = true
	}
	if doc.paintDirty {
		doc.paint(canvas)
	}
	doc.clean()
}

func (doc *Document) SetDelegator(d Delegator) {
	doc.delegator = d
}
func (doc *Document) handleKeyboardEvent(event KeyboardEventArgs) {
	if doc.delegator == nil {
		return
	}
	doc.delegator.HandleKeyboardEvent(event.Name, event.Pressed)
}

// 获取指定ID的元素。
func (doc *Document) GetElementByID(id string) Box {
	var out Box
	doc.walkNode(doc.root, func(box Box) bool {
		if box.Base().ID == id {
			out = box
			return false
		}
		return true
	})
	return out
}

// 根据CSS选元素。
// 找不到返回空，错误的selector直接崩溃。
func (doc *Document) QuerySelector(selector string) Box {
	var outBox Box
	sel := ParseSelector(selector)
	doc.walkNode(doc.root, func(box Box) bool {
		if (_Styler{doc}).match(box, sel) {
			outBox = box
			return false
		}
		return true
	})
	return outBox
}

// 选择所有匹配的元素。
func (doc *Document) QuerySelectorAll(selector string) []Box {
	var outBoxes []Box
	sel := ParseSelector(selector)
	doc.walkNode(doc.root, func(box Box) bool {
		if (_Styler{doc}).match(box, sel) {
			outBoxes = append(outBoxes, box)
		}
		return true
	})
	return outBoxes
}

type _NodeTransformer struct {
	doc *Document
}

func (n _NodeTransformer) Transform(node *html.Node) (Box, error) {
	return n.transform(nil, node)
}

func (n _NodeTransformer) transform(parent Box, node *html.Node) (Box, error) {
	switch node.Type {
	case html.ElementNode:
		switch node.Data {
		case `block`:
			return n.transformNode(NewBlock(n.doc), node, false, false)
		case `inline`:
			return n.transformNode(NewInline(n.doc), node, false, false)
		case `stack`:
			return n.transformNode(NewStack(n.doc), node, false, false)
		case `scroll`:
			return n.transformNode(NewScroll(n.doc), node, false, false)
		case `spacer`:
			return n.transformNode(NewSpacer(n.doc), node, true, false)
		case `button`:
			return n.transformNode(NewButton(n.doc), node, false, false)
		case `img`:
			return n.transformNode(NewImage(n.doc), node, true, false)
		case `text`:
			text, err := n.transformNode(NewText(n.doc), node, false, true)
			if err == nil {
				text.(*Text).expandTextNodes()
			}
			return text, err
		case `b`, `i`:
			if parent == nil {
				return nil, fmt.Errorf(`此处不能有元素：%s`, node.Data)
			}
			switch parent.Base().Tag {
			case `text`, `b`, `i`:
			default:
				return nil, fmt.Errorf(`父子关系不正确：%s -> %s`, parent.Base().Tag, node.Data)
			}
			var box Box
			switch node.Data {
			case `b`:
				box = NewBoldText(n.doc)
			case `i`:
				box = NewItalicText(n.doc)
			default:
				panic(`未处理的节点`)
			}
			return n.transformNode(box, node, false, true)
		}
	}
	return nil, fmt.Errorf(`未识别的标签：%v`, node.Data)
}

// 只处理元素节点，文本节点此内部处理了，不会调用自身。
func (n _NodeTransformer) transformNode(box Box, node *html.Node, voidElement bool, allowText bool) (Box, error) {
	for _, a := range node.Attr {
		// 所有的节点理应都是从BaseBox继承的，所以接口不可能为空。
		// 但是也不能调BaseBox().Set...，因为子类有方法覆盖。
		if err := box.(Setter).Set(a.Key, a.Val); err != nil {
			return nil, err
		}
	}
	// 不能有子节点的节点
	if voidElement && node.FirstChild != nil {
		return nil, fmt.Errorf(`节点不能包含子节点：%s`, node.Data)
	}
	// 看起来全部合法了？开始处理。
	for childNode := range node.ChildNodes() {
		var childBoxOrString any
		if childNode.Type == html.TextNode {
			trimmed := strings.TrimSpace(childNode.Data)
			if !allowText && trimmed != `` {
				return nil, fmt.Errorf(`此处不能有文本节点：%s`, childNode.Data)
			}
			// 空文本总是忽略。
			if trimmed == `` {
				continue
			}
			childBoxOrString = trimmed
		} else if childNode.Type == html.CommentNode {
			continue
		} else {
			child, err := n.transform(box, childNode)
			if err != nil {
				return nil, err
			}
			childBoxOrString = child
		}

		if text, ok := box.(interface {
			AppendChild(child any)
		}); ok {
			text.AppendChild(childBoxOrString)
		} else {
			box.Base().AppendChild(childBoxOrString.(Box))
		}
	}
	return box, nil
}

// 为节点计算样式。
func (doc *Document) style(box Box, descendents bool) error {
	styler := _Styler{doc: doc}
	return styler.Style(box, descendents, doc.styleSheet)
}

// 重新布局整个文档。
//
// TODO 把计算方式从元素自身拆解到这里来。
func (doc *Document) layout() {
	doc.root.Calc(doc.width, doc.height)
}

// 绘制文档。
func (doc *Document) paint(canvas *Canvas) {
	doc.root.Draw(canvas)
}

func (doc *Document) walkNode(box Box, callback func(box Box) bool) bool {
	if !callback(box) {
		return false
	}
	for _, child := range box.Base().Children {
		if !doc.walkNode(child, callback) {
			return false
		}
	}
	return true
}

// TODO 异步解码
func (doc *Document) loadImage(src string) (DecodedImage, error) {
	if !strings.Contains(src, `:`) {
		return doc.imageManager.GetImageCached(doc.fsys, path.Join(doc.skinDir, src))
	}
	if u, err := url.Parse(src); err == nil {
		switch u.Scheme {
		case `os`:
			if u, err := url.PathUnescape(u.Opaque); err == nil {
				if filepath.IsAbs(u) {
					return doc.imageManager.GetImageCached(os.DirFS(`/`), u[1:])
				} else {
					return doc.imageManager.GetImageCached(os.DirFS(`.`), u)
				}
			}
		}
	}

	return DecodedImage{}, fmt.Errorf(`不支持的来源：%s`, src)
}

func (doc *Document) loadFaceWithFallback(box Box) *FontFace {
	return doc.fontManager.GetFaceWithFallback(
		box.Base().computedStyles.FontFamily.String,
		box.Base().computedStyles.FontSize.Number,
		box.Base().computedStyles.FontBold.Bool,
		box.Base().computedStyles.FontItalic.Bool,
	)
}

// 这个类的存在只是不想document下有太多不相关的方法。
type _Styler struct {
	doc *Document
}

func (s _Styler) Style(box Box, descendents bool, sheet *Sheet) (outErr error) {
	s.doc.walkNode(box, func(box Box) bool {
		var rules []RuleMatch
		if DefaultStyles != nil {
			rules = append(rules, s.findRulesFor(box, DefaultStyles)...)
		}
		if sheet != nil {
			rules = append(rules, s.findRulesFor(box, sheet)...)
		}
		if err := s.computeStyles(box, rules); err != nil {
			outErr = fmt.Errorf(`样式应用失败：%w`, err)
			return false
		}
		return descendents
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

	// 从空开始。
	styles := Styles{}

	inlines := &node.Base().inlineStyles
	inlineValue := reflect.ValueOf(inlines)
	stylesValue := reflect.ValueOf(&styles)
	documentStylesValue := reflect.ValueOf(&s.doc.defaultStyles)

	// 从父母继承
	// TODO 优化：如果样式表或内联表有值，则无需再从父母继承。
	for field, value := range stylesValue.Elem().Fields() {
		if !ShouldInherit(field.Name) {
			continue
		}
		if field.Type == reflect.TypeFor[Value]() {
			setFromParent := false
			for parent := node.Base().Parent; parent != nil; parent = parent.Base().Parent {
				parentValue := reflect.ValueOf(&parent.Base().computedStyles)
				parentField := parentValue.Elem().FieldByIndex(field.Index)
				value3 := parentField.Interface().(Value)
				if !value3.Empty() {
					value.Set(parentField)
					setFromParent = true
					// 从最近的祖先那里获取一次即可。
					break
				}
			}
			// <document> 是所有元素的父节点。
			if !setFromParent {
				docField := documentStylesValue.Elem().FieldByIndex(field.Index)
				docValue := docField.Interface().(Value)
				if !docValue.Empty() {
					value.Set(docField)
				}
			}
		}
	}

	// 从样式表更新
	for d := range declarations(rules) {
		// 处理样式计算过程中，结果可以直接丢。
		if _, _, _, err := styles.Set(d.Name, d.Value); err != nil {
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
