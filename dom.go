package fbiw

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log"
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
	// layout 变化一定会重绘。
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
	root, sheet, err := parseDocument(doc, fp)
	if err != nil {
		return fmt.Errorf(`文档解析失败：%w`, err)
	}
	doc.root = root
	doc.styleSheet = sheet

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
//
// Unmarshal 那边也要用，所以独立出来。
func parseDocument(owner *Document, content io.Reader) (Box, *Sheet, error) {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: `div`}
	nodes, err := html.ParseFragment(content, context)
	if err != nil {
		return nil, nil, fmt.Errorf(`文档解析失败：%w`, err)
	}
	// 去掉根节点所有的文本节点，确保只有一个节点。
	nodes = slices.DeleteFunc(nodes, func(node *html.Node) bool {
		return node.Type != html.ElementNode
	})
	if len(nodes) != 1 {
		return nil, nil, fmt.Errorf(`找不到文档的元素节点`)
	}

	first := nodes[0]
	if first.Data != `document` {
		return nil, nil, fmt.Errorf(`根节点需要是 <document>`)
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
					return nil, nil, fmt.Errorf(`重复的样式节点`)
				}
				styleNode = child
			} else if child.Data == `block` {
				if bodyNode != nil {
					return nil, nil, fmt.Errorf(`根元素下重复节点`)
				}
				bodyNode = child
			} else if child.Data == `inline` {
				if bodyNode != nil {
					return nil, nil, fmt.Errorf(`根元素下重复节点`)
				}
				bodyNode = child
			} else {
				return nil, nil, fmt.Errorf(`根元素下不认识的节点：%s`, child.Data)
			}
		case html.TextNode:
			if strings.TrimSpace(child.Data) != `` {
				return nil, nil, fmt.Errorf(`根元素下不能有文本内容`)
			}
		case html.CommentNode:
		}
	}

	var sheet *Sheet

	if styleNode != nil {
		if styleNode.FirstChild == nil {
			sheet = &Sheet{}
		} else if styleNode.FirstChild.NextSibling != nil {
			return nil, nil, fmt.Errorf(`样式格式错误`)
		} else if styleNode.FirstChild.Type != html.TextNode {
			return nil, nil, fmt.Errorf(`不是文本节点`)
		} else {
			textData := styleNode.FirstChild.Data
			sheet2, err := ParseStyle([]byte(textData))
			if err != nil {
				return nil, nil, fmt.Errorf(`样式解析失败：%w`, err)
			}
			sheet = sheet2
		}
	}

	if bodyNode == nil {
		return nil, nil, fmt.Errorf(`缺少block节点`)
	}

	box, err := _NodeTransformer{owner}.Transform(bodyNode)
	if err != nil {
		return nil, nil, fmt.Errorf(`文档内容节点解析失败：%w`, err)
	}

	return box, sheet, nil
}

// 反序列化content(html)到指定结构体中。
//
//   - 必须要有一个 `Root fbiw.Box` 元素，用来保存根节点。
//
//   - 其它的写法形如： Txt *fbiw.Text `css:"text"`，
//     表示在文档中基于css的选择器找到元素并赋值给 Txt。
//
//     名字需要是已导出的字段（大写字母开头）。
//
// 返回指针类型。
func Unmarshal[T any](owner *Document, content string) *T {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(`<document>`)
	buf.WriteString(content)
	buf.WriteString(`</document>`)

	root, _, err := parseDocument(owner, buf)
	if err != nil {
		panic(err)
	}

	var t T
	rv := reflect.ValueOf(&t).Elem()
	for field, fieldValue := range rv.Fields() {
		if !field.IsExported() {
			continue
		}

		if field.Name == `Root` {
			if field.Type != reflect.TypeFor[Box]() {
				panic(`Root必须是Box`)
			}
			fieldValue.Set(reflect.ValueOf(root))
			continue
		}

		if !field.Type.Implements(reflect.TypeFor[Box]()) {
			continue
		}

		selector := field.Tag.Get(`css`)
		if selector == `` {
			continue
		}

		parsedSelector := ParseSelector(selector)
		var outBox Box
		owner.walkNode(root, func(box Box) bool {
			if (_Styler{owner}).match(box, parsedSelector) {
				outBox = box
				return false
			}
			return true
		})
		if outBox == nil {
			continue
		}

		if !reflect.ValueOf(outBox).CanConvert(field.Type) {
			log.Panicf(`类型不匹配：%v vs. %T`, field.Type.String(), outBox)
		}

		converted := reflect.ValueOf(outBox).Convert(field.Type)
		fieldValue.Set(converted)
	}

	return &t
}

// 找到此结构体中名为 Root 类型为 Box 的字段。
// func unmarshalGetRoot(t any) Box {
// 	rv := reflect.ValueOf(t).Elem()
// 	root := rv.FieldByName(`Root`)
// 	return root.Interface().(Box)
// }

// 标记文档内容脏掉了，需要重绘。
// 但是文档本身不强关联app框架的，所以按需标记给app更新。
func (doc *Document) RequestLayout() {
	doc.layoutDirty = true
	if doc.app != nil {
		doc.app.Dirty()
	}
}

func (doc *Document) RequestPaint() {
	doc.paintDirty = true
	if doc.app != nil {
		doc.app.Dirty()
	}
}

// 作用同 RequestLayout，但是可以在线程中调用。
//
// 必须在文档绑定App框架后可用。
func (doc *Document) RequestLayoutAsync() {
	if doc.app == nil {
		log.Panicln(`Document.RequestLayoutAsync 未绑定 App`)
	}
	doc.app.Async(func() {
		doc.RequestLayout()
	})
}

// 作用同 RequestPaint，但是可以在线程中调用。
//
// 必须在文档绑定App框架后可用。
func (doc *Document) RequestPaintAsync() {
	if doc.app == nil {
		log.Panicln(`Document.RequestPaintAsync 未绑定 App`)
	}
	doc.app.Async(func() {
		doc.RequestPaint()
	})
}

// 需要重新布局或者重新绘制？
func (doc *Document) dirty() bool {
	return doc.layoutDirty || doc.paintDirty
}

// 清理不干净的布局和重绘状态。
func (doc *Document) clean() {
	doc.layoutDirty = false
	doc.paintDirty = false
}

func (doc *Document) sync(canvas *Canvas, forcePaint bool) {
	// 如果文档之上（多窗口混合的时候）还有其它文档，则
	// 即便本文档是干净的，也会被上面的玷污，所以强制更新。
	if forcePaint {
		doc.paintDirty = true
	}

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
func (doc *Document) GetBoxByID(id string) Box {
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
		default:
			if defined, ok := _definedBoxes[node.Data]; ok {
				return n.transformNode(defined.new(n.doc), node, defined.void, defined.text)
			}
		}
	}
	return nil, fmt.Errorf(`未识别的标签：%v`, node.Data)
}

// 只处理元素节点，文本节点此内部处理了，不会调用自身。
func (n _NodeTransformer) transformNode(box Box, node *html.Node, voidElement bool, allowText bool) (Box, error) {
	for _, a := range node.Attr {
		// 所有的节点理应都是从BaseBox继承的，所以接口不可能为空。
		// 但是也不能调BaseBox().Set...，因为子类有方法覆盖。
		if err := box.(PropertySetter).SetProp(a.Key, a.Val); err != nil {
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
	// doc.root.Base().computedStyles.Width = NumberValue(doc.width)
	// doc.root.Base().computedStyles.Height = NumberValue(doc.height)
	doc.root.Calc(doc.width, doc.height, Constraints{
		PrefersMaxWidth:  true,
		PrefersMaxHeight: true,
	})
}

// 绘制文档。
func (doc *Document) paint(canvas *Canvas) {
	doc.root.Draw(canvas)
}

// 此方法只是属于doc，但是并不使用doc。
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
// width, height 表示想要scale到的尺寸。
// 如果均为0，则表示不scale。
// checking: 只检测是否存在缓存。
func (doc *Document) _loadImage(src string, width, height int, checking bool) (DecodedImage, error) {
	if !strings.Contains(src, `:`) {
		return doc.imageManager.GetImageScaledCached(doc.fsys, path.Join(doc.skinDir, src), width, height, checking)
	}
	if u, err := url.Parse(src); err == nil {
		switch u.Scheme {
		case `os`:
			if u, err := url.PathUnescape(u.Opaque); err == nil {
				if filepath.IsAbs(u) {
					return doc.imageManager.GetImageScaledCached(os.DirFS(`/`), u[1:], width, height, checking)
				} else {
					return doc.imageManager.GetImageScaledCached(os.DirFS(`.`), u, width, height, checking)
				}
			}
		}
	}

	return DecodedImage{}, fmt.Errorf(`不支持的来源：%s`, src)
}

// 同步加载图片，如果没有缓存，返回不存在。
func (doc *Document) loadImageSync(src string, width, height int) (DecodedImage, error) {
	return doc._loadImage(src, width, height, true)
}

// 异步加载图片，回调发生在主线程中，可安全地修改盒子内容。
func (doc *Document) loadImageAsync(src string, width, height int, callback func(DecodedImage, error)) {
	go func() {
		img, err := doc._loadImage(src, width, height, false)
		doc.app.Async(func() {
			callback(img, err)
		})
	}()
}

func (doc *Document) LoadFaceWithFallback(box Box) *FontFace {
	return doc.fontManager.GetFaceWithFallback(
		box.Base().computedStyles.FontFamily.String,
		box.Base().computedStyles.FontSize.Number,
		box.Base().computedStyles.FontBold.Bool,
		box.Base().computedStyles.FontItalic.Bool,
	)
}

type _BoxRegistryItem struct {
	name string
	void bool
	text bool
	new  func(doc *Document) Box
}

var _definedBoxes = map[string]_BoxRegistryItem{}

// 创建用户自定义组件。
func Define[T Box](name string, void bool, new func(doc *Document) T) {
	if _, ok := _definedBoxes[name]; ok {
		log.Panicf(`盒子重复定义: %s`, name)
	}
	_definedBoxes[name] = _BoxRegistryItem{
		name: name,
		void: void,
		text: false,
		new: func(doc *Document) Box {
			return new(doc)
		},
	}
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
		(len(selector.Class) == 0 || node.Base().class.ContainsAll(selector.Class...)) &&
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
