package fbiw

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/goccy/go-yaml"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func newFlexTestDocument(t *testing.T, body string, width, height int) *Document {
	t.Helper()
	fm := NewFontManager()
	fm.faces[_FontFaceKey{Family: `system`, Size: 32}] = &FontFace{
		Face: basicfont.Face7x13, cache: map[rune]GlyphValue{},
	}
	doc := _NewDocument(width, height, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document>` + body + `</document>`)},
	}, fm, nil)
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	doc.layout()
	return doc
}

func TestFlexLayout(t *testing.T) {
	for _, tc := range []struct {
		name, html    string
		width, height int
		want          map[string]Rect
	}{
		{"flex root", `<flex gap="10"><block id="a" width="10" flex-grow="1"></block><block id="b" width="20" flex-grow="2"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 30, 40}, "b": {40, 0, 60, 40}}},
		{"nested flex tags", `<flex><flex id="a" width="20" flex-grow="1"><block id="inner" width="0" flex-grow="1"></block></flex><block id="b" width="20"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 80, 40}, "inner": {0, 0, 80, 40}, "b": {80, 0, 20, 40}}},
		{"weighted growth", `<flex gap="10"><block id="a" width="10" flex-grow="1"></block><block id="b" width="20" flex-grow="2"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 30, 40}, "b": {40, 0, 60, 40}}},
		{"column", `<flex flex-direction="column" gap="10"><block id="a" height="10" flex-grow="1"></block><block id="b" height="20" flex-grow="2"></block></flex>`, 80, 100,
			map[string]Rect{"a": {0, 0, 80, 30}, "b": {0, 40, 80, 60}}},
		{"rounding", `<flex><block id="a" width="0" flex-grow="1"></block><block id="b" width="0" flex-grow="1"></block><block id="c" width="0" flex-grow="1"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 33, 40}, "b": {33, 0, 33, 40}, "c": {66, 0, 34, 40}}},
		{"insets", `<flex padding="5" border-width="1" justify-content="space-between" align-items="center"><block id="a" width="20" height="10"></block><block id="b" width="30" height="20"></block></flex>`, 100, 60,
			map[string]Rect{"a": {6, 25, 20, 10}, "b": {64, 20, 30, 20}}},
		{"self alignment and explicit zero", `<flex align-items="end"><block id="a" width="20" height="10" align-self="center"></block><block id="b" width="20" height="10"></block><block id="c" width="20" height="0" align-self="stretch"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 15, 20, 10}, "b": {20, 30, 20, 10}, "c": {40, 0, 20, 0}}},
		{"overflow", `<flex gap="5"><block id="a" width="30" height="30"></block><block id="b" width="30" height="30"></block></flex>`, 40, 20,
			map[string]Rect{"a": {0, 0, 30, 30}, "b": {35, 0, 30, 30}}},
		{"hidden children do not add gaps", `<flex gap="5"><block id="a" width="20"></block><block display="false" width="90" flex-grow="99"></block><block id="b" width="20"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 20, 40}, "b": {25, 0, 20, 40}}},
		{"nested flex", `<flex><flex id="a" width="20" flex-grow="1"><block id="inner" width="0" flex-grow="1"></block></flex><block id="b" width="20"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 80, 40}, "inner": {0, 0, 80, 40}, "b": {80, 0, 20, 40}}},
		{"flex gap", `<flex gap="5"><block id="a" width="20"></block><block id="b" width="20"></block></flex>`, 100, 40,
			map[string]Rect{"a": {0, 0, 20, 40}, "b": {25, 0, 20, 40}}},
		{"content sized container", `<block><flex id="outer" align-items="start" gap="5"><block id="a" width="20" height="10"></block><block id="b" width="30" height="20"></block></flex></block>`, 100, 40,
			map[string]Rect{"outer": {0, 0, 100, 20}, "a": {0, 0, 20, 10}, "b": {25, 0, 30, 20}}},
		{"empty", `<flex id="empty" justify-content="space-around" gap="20"></flex>`, 0, 0,
			map[string]Rect{"empty": {0, 0, 0, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := newFlexTestDocument(t, tc.html, tc.width, tc.height)
			for id, want := range tc.want {
				if got := doc.GetBoxByID[Box](id).GetLayoutBox(); got != want {
					t.Errorf("%s: got %+v, want %+v", id, got, want)
				}
			}
		})
	}
}

func TestFlexBox(t *testing.T) {
	flex := NewFlex(nil)
	var box Box = flex
	child := NewBlock(nil)
	child.computedStyles.SetWidth(NumberLength(0))
	child.computedStyles.SetFlexGrow(1)
	flex.children = []Box{child}
	box.Calc(100, 40, Constraints{PrefersMaxWidth: true, PrefersMaxHeight: true})
	if got := child.GetLayoutBox(); got != (Rect{0, 0, 100, 40}) {
		t.Fatalf("NewFlex layout: %+v", got)
	}

	doc := newFlexTestDocument(t, `<style>flex { flex-direction: column; gap: 5; }</style><flex id="root"><block id="a" width="20" height="10"></block><block id="b" width="30" height="10"></block></flex>`, 100, 40)
	root := doc.GetBoxByID[*Flex]("root")
	if root == nil {
		t.Fatal("flex tag did not create *Flex")
	}
	b := doc.GetBoxByID[Box]("b")
	if got := b.GetLayoutBox(); got.X != 0 || got.Y != 15 {
		t.Fatalf("column flex: %+v", got)
	}
	if err := root.SetProp("display", "inline"); err == nil {
		t.Fatal("display must not change layout type")
	}
	if err := root.SetProp("display", "false"); err != nil {
		t.Fatal(err)
	}
	if displaying(root) {
		t.Fatal("flex was not hidden")
	}
	if err := root.SetProp("display", "true"); err != nil {
		t.Fatal(err)
	}
	doc.layout()
	if got := b.GetLayoutBox(); got.X != 0 || got.Y != 15 {
		t.Fatalf("restored default flex: %+v", got)
	}
}

func TestFlexJustification(t *testing.T) {
	for _, tc := range []struct {
		mode          string
		first, second int
	}{
		{"start", 0, 20}, {"end", 60, 80}, {"center", 30, 50},
		{"space-between", 0, 80}, {"space-around", 15, 65}, {"space-evenly", 20, 60},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			doc := newFlexTestDocument(t, `<flex justify-content="`+tc.mode+`"><block id="a" width="20"></block><block id="b" width="20"></block></flex>`, 100, 40)
			if a, b := doc.GetBoxByID[Box]("a").GetLayoutBox(), doc.GetBoxByID[Box]("b").GetLayoutBox(); a.X != tc.first || b.X != tc.second {
				t.Fatalf("positions = %d, %d; want %d, %d", a.X, b.X, tc.first, tc.second)
			}
		})
	}
}

func TestFlexRelayoutPreservesStyles(t *testing.T) {
	doc := newFlexTestDocument(t, `<flex gap="10"><block id="a" width="25%" flex-grow="1"></block><block id="b" width="20"></block></flex>`, 100, 40)
	a := doc.GetBoxByID[Box]("a")
	before := *a.GetComputedStyles()
	for _, width := range []int{100, 200, 100, 100} {
		doc.width = width
		doc.layout()
		if got := a.GetLayoutBox().Width; got != width-30 {
			t.Fatalf("width = %d, want %d", got, width-30)
		}
		if !reflect.DeepEqual(*a.GetComputedStyles(), before) {
			t.Fatal("layout modified styles")
		}
	}
	doc.layoutDirty = false
	if err := a.SetProp("flex-grow", "0"); err != nil {
		t.Fatal(err)
	}
	if !doc.layoutDirty {
		t.Fatal("flex-grow change did not invalidate layout")
	}
	doc.layout()
	if got := a.GetLayoutBox().Width; got != 25 {
		t.Fatalf("width after removing grow = %d", got)
	}
}

func TestFlexTextReflowsAtAllocatedWidth(t *testing.T) {
	doc := newFlexTestDocument(t, `<flex align-items="start"><text id="text" width="14" flex-grow="1">ABCDEFGHIJKL</text><block width="14"></block></flex>`, 98, 100)
	text := doc.GetBoxByID[*Text]("text")
	if text.layoutBox.Width != 84 || len(text.textLines) != 1 {
		t.Fatalf("wide text: box %+v, lines %d", text.layoutBox, len(text.textLines))
	}
	doc.width = 42
	doc.layout()
	if text.layoutBox.Width != 28 || len(text.textLines) != 3 {
		t.Fatalf("narrow text: box %+v, lines %d", text.layoutBox, len(text.textLines))
	}
	if text.computedStyles.Width != NumberLength(14) {
		t.Fatal("text width style changed")
	}
}

func TestFlexWidgetAllocation(t *testing.T) {
	for _, tag := range []string{`toggle`, `progress`, `select`, `img`, `scroll`} {
		t.Run(tag, func(t *testing.T) {
			doc := newFlexTestDocument(t, `<flex><`+tag+` id="item" width="10" height="5" padding="0" flex-grow="1"></`+tag+`></flex>`, 100, 40)
			box := doc.GetBoxByID[Box](`item`)
			if got := box.GetLayoutBox(); got.Width != 100 || got.Height != 5 {
				t.Fatalf("allocated box: %+v", got)
			}
			if got := box.GetComputedStyles().Width; got != NumberLength(10) {
				t.Fatalf("style changed: %+v", got)
			}
		})
	}
}

func TestFixedDimensionsOverrideStyles(t *testing.T) {
	b := NewBlock(nil)
	b.computedStyles.SetWidth(PercentageLength(50))
	b.computedStyles.SetHeight(NumberLength(80))
	size := b.resolveDimensions(Constraints{
		ParentContentWidth: 200, ParentContentHeight: 100,
		FixedWidth: NumberLength(0), FixedHeight: NumberLength(20),
	})
	if size.Width != NumberLength(0) || size.Height != NumberLength(20) {
		t.Fatalf("size: %+v", size)
	}
	if b.computedStyles.Width != PercentageLength(50) || b.computedStyles.Height != NumberLength(80) {
		t.Fatal("styles modified")
	}
	img := NewImage(nil)
	img.src = `cached`
	img.status = imageLoadStatusScaled
	img.decodedImage = DecodedImage{Width: 20, Height: 10}
	img.Calc(0, 0, Constraints{FixedWidth: NumberLength(0), FixedHeight: NumberLength(0)})
	if got := img.GetLayoutBox(); got.Width != 0 || got.Height != 0 {
		t.Fatalf("image ignored fixed zero: %+v", got)
	}
}

func TestDisplayHidesRootAndDescendants(t *testing.T) {
	for _, tag := range []string{`block`, `inline`, `flex`, `stack`} {
		t.Run(tag, func(t *testing.T) {
			doc := newFlexTestDocument(t, `<`+tag+` id="root" background-color="red"><block id="child" width="10" height="10" display="true" background-color="blue"></block></`+tag+`>`, 20, 20)
			root := doc.root
			child := doc.GetBoxByID[Box](`child`)
			if !root.GetComputedStyles().Display {
				t.Fatal("root should default to visible")
			}
			doc.layoutDirty = false
			if err := root.SetProp(`display`, `false`); err != nil {
				t.Fatal(err)
			}
			if !doc.layoutDirty || displaying(root) {
				t.Fatal("hide must invalidate layout and hide root")
			}
			if !child.GetComputedStyles().Display {
				t.Fatal("display must not inherit from parent")
			}
			doc.layout()
			canvas := NewCanvas(20, 20)
			doc.paint(canvas)
			if !reflect.DeepEqual(canvas.buffer, make([]byte, len(canvas.buffer))) {
				t.Fatal("hidden root or its child was painted")
			}
			if err := root.SetProp(`display`, `true`); err != nil {
				t.Fatal(err)
			}
			doc.layout()
			doc.paint(canvas)
			if reflect.DeepEqual(canvas.buffer, make([]byte, len(canvas.buffer))) {
				t.Fatal("root was not painted after showing it")
			}
		})
	}
}

func TestDisplayToggleRelayout(t *testing.T) {
	doc := newFlexTestDocument(t, `<flex gap="5"><block id="a" width="20"></block><block id="b" width="20"></block></flex>`, 100, 40)
	a, b := doc.GetBoxByID[Box](`a`), doc.GetBoxByID[Box](`b`)
	for _, tc := range []struct {
		display string
		x       int
	}{{"false", 0}, {"true", 25}, {"0", 0}, {"1", 25}} {
		if err := a.SetProp(`display`, tc.display); err != nil {
			t.Fatal(err)
		}
		doc.layout()
		if got := b.GetLayoutBox().X; got != tc.x {
			t.Fatalf("display=%s: second item X=%d, want %d", tc.display, got, tc.x)
		}
	}
}

func TestResolveLayoutLength(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     Length
		reference int
		want      Length
	}{
		{"unset", Length{}, 200, Length{}},
		{"zero", NumberLength(0), 200, NumberLength(0)},
		{"pixels", NumberLength(17), 200, NumberLength(17)},
		{"percentage", PercentageLength(50), 200, NumberLength(100)},
		{"round down", PercentageLength(33), 101, NumberLength(33)},
		{"zero reference", PercentageLength(50), 0, NumberLength(0)},
		{"negative reference", PercentageLength(50), -10, NumberLength(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveLayoutLength(tc.value, tc.reference); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPercentageDimensionsRelayout(t *testing.T) {
	containers := map[string]func(*Document) Box{
		"block":  func(d *Document) Box { return NewBlock(d) },
		"inline": func(d *Document) Box { return NewInline(d) },
		"stack":  func(d *Document) Box { return NewStack(d) },
	}
	children := map[string]func(*Document) Box{
		"block":    containers["block"],
		"inline":   containers["inline"],
		"stack":    containers["stack"],
		"image":    func(d *Document) Box { return NewImage(d) },
		"scroll":   func(d *Document) Box { return NewScroll(d) },
		"toggle":   func(d *Document) Box { return NewToggle(d) },
		"progress": func(d *Document) Box { return NewProgressBar(d) },
		"select":   func(d *Document) Box { return NewSelectBox(d) },
	}
	for parentName, newParent := range containers {
		for childName, newChild := range children {
			t.Run(parentName+"/"+childName, func(t *testing.T) {
				doc := _NewDocument(200, 120, nil, nil, nil)
				parent := newParent(doc)
				doc.root = parent
				parent.Base().computedStyles.SetPadding(PaddingValue(10, 10, 10, 10))
				parent.Base().computedStyles.SetBorderWidth(2)
				first := NewBlock(doc)
				first.computedStyles.SetWidth(NumberLength(80))
				first.computedStyles.SetHeight(NumberLength(20))
				child := newChild(doc)
				child.Base().computedStyles.SetWidth(PercentageLength(50))
				child.Base().computedStyles.SetHeight(PercentageLength(50))
				before := child.Base().computedStyles
				parent.Base().children = []Box{first, child}
				// 不重新计算样式，覆盖扩大、缩小和同尺寸重复布局。
				for _, dims := range [][2]int{{200, 120}, {320, 200}, {200, 120}, {200, 120}} {
					doc.width, doc.height = dims[0], dims[1]
					doc.layout()
					got := child.GetLayoutBox()
					// 百分比基于完整内容区，扣除 padding/border，但不扣除 first。
					wantW, wantH := (dims[0]-24)/2, (dims[1]-24)/2
					if got.Width != wantW || got.Height != wantH {
						t.Fatalf("document %v: got %+v, want size %dx%d", dims, got, wantW, wantH)
					}
					if !reflect.DeepEqual(child.Base().computedStyles, before) {
						t.Fatal("layout mutated computed styles")
					}
				}
			})
		}
	}
}

func TestRootAndNestedPercentageDimensions(t *testing.T) {
	doc := _NewDocument(200, 120, nil, nil, nil)
	root := NewBlock(doc)
	child := NewBlock(doc)
	doc.root = root
	root.children = []Box{child}
	for _, box := range []*Block{root, child} {
		box.computedStyles.SetWidth(PercentageLength(50))
		box.computedStyles.SetHeight(PercentageLength(50))
	}
	for _, dims := range [][2]int{{200, 120}, {400, 240}} {
		doc.width, doc.height = dims[0], dims[1]
		doc.layout()
		for i, box := range []*Block{root, child} {
			divisor := 2 << i
			if got := box.GetLayoutBox(); got.Width != dims[0]/divisor || got.Height != dims[1]/divisor {
				t.Fatalf("level %d: got %+v", i, got)
			}
			if !box.computedStyles.Width.IsPercentage() || !box.computedStyles.Height.IsPercentage() {
				t.Fatal("percentage was lost")
			}
		}
	}
}

func TestPercentageDimensionsForFlexibleChild(t *testing.T) {
	for _, parent := range []Box{NewBlock(nil), NewInline(nil)} {
		t.Run(parent.Base().Tag, func(t *testing.T) {
			child := NewBlock(nil)
			child.computedStyles.SetSpacer(true)
			child.computedStyles.SetWidth(PercentageLength(50))
			child.computedStyles.SetHeight(PercentageLength(50))
			parent.Base().children = []Box{child}
			for _, dims := range [][2]int{{200, 120}, {400, 240}} {
				parent.Calc(dims[0], dims[1], Constraints{
					PrefersMaxWidth: true, PrefersMaxHeight: true,
				})
				if got := child.GetLayoutBox(); got.Width != dims[0]/2 || got.Height != dims[1]/2 {
					t.Fatalf("got %+v, want half of %v", got, dims)
				}
			}
		})
	}
}

func TestScrollSlotPercentageDimensions(t *testing.T) {
	slot := _NewScrollChild(nil)
	slot.computedStyles.SetPadding(PaddingValue(2, 2, 2, 2))
	child := NewBlock(nil)
	child.computedStyles.SetWidth(PercentageLength(50))
	child.computedStyles.SetHeight(PercentageLength(50))
	slot.children = []Box{child}
	for _, dims := range [][2]int{{200, 120}, {400, 240}} {
		slot.forceCalc(0, 0, dims[0], dims[1], true)
		if got := child.GetLayoutBox(); got.Width != (dims[0]-4)/2 || got.Height != (dims[1]-4)/2 {
			t.Fatalf("got %+v, want half of slot content area %v", got, dims)
		}
	}
}

func TestQuery(t *testing.T) {
	type _Test struct {
		Selector string
		HTML     string
		Boxes    []string
	}

	cases := loadTestCases[_Test](`testdata/query.yaml`)
	for i, tc := range cases {
		// 早期非标准页面兼容
		if strings.HasPrefix(tc.HTML, `<block`) || strings.HasPrefix(tc.HTML, `<inline`) {
			updated := `<document><style>` +
				`</style>` +
				tc.HTML +
				`</document>`
			tc.HTML = updated
		}

		doc := _NewDocument(
			1024, 768,
			fstest.MapFS{
				`main.html`: &fstest.MapFile{
					Data: []byte(tc.HTML),
					Mode: 0644,
				},
			},
			nil, nil,
		)
		if err := doc.load(`main.html`); err != nil {
			t.Errorf(`文档解析失败：#%d: %v`, i, err)
			continue
		}

		boxes := doc.QuerySelectorAll[Box](tc.Selector)
		if len(boxes) != len(tc.Boxes) {
			t.Errorf(`选择结果数不相等: #%d: %d vs. %d`, i+1, len(tc.Boxes), len(boxes))
			continue
		}
		for i := range len(boxes) {
			id1 := boxes[i].Base().ID
			id2 := tc.Boxes[i]
			if id1 != id2 {
				t.Errorf(`盒子ID不一样: #%d: %s vs. %s`, i, id2, id1)
			}
		}
	}
}

func TestBind(t *testing.T) {
	doc := _NewDocument(100, 100, nil, nil, nil)
	parsed, err := parseDocument(doc, strings.NewReader(`
<document>
<block>
	<inline>
		<text>111</text>
	</inline>
	<inline>
		<img><img><img>
	</inline>
</block>
</document>
	`))
	if err != nil {
		t.Fatal(err)
	}
	to := struct {
		root    Box
		text    *Text    `css:"text"`
		images1 []Box    `css:"img"`
		images2 []*Image `css:"img"`
	}{}
	Bind(&to, parsed.root)
	if to.root == nil {
		panic(`root == nil`)
	}
	if to.text == nil || to.text.GetText() != `111` {
		panic(`错误`)
	}
	if len(to.images1) != 3 || len(to.images2) != 3 {
		panic(`个数错误`)
	}
}

type expectedStyleValue struct {
	value any
}

func (v *expectedStyleValue) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err != nil {
		return err
	}
	before, after, ok := strings.Cut(s, `:`)
	if !ok {
		return fmt.Errorf(`无效Value: %s`, s)
	}
	switch before {
	case `string`:
		v.value = after
		return nil
	case `bool`:
		v.value = after == `true`
		return nil
	case `number`:
		n, err := strconv.ParseInt(after, 10, 64)
		v.value = NumberLength(n)
		return err
	case `color`:
		if preset, ok := presetColors[after]; ok {
			v.value = Color(preset)
			return nil
		}
	}
	return fmt.Errorf(`未知值类型: %s`, s)
}

func loadTestCases[T any](path string) []*T {
	fp, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer fp.Close()

	var cases []*T
	if err := yaml.NewDecoder(fp, yaml.DisallowUnknownField()).Decode(&cases); err != nil {
		panic(err)
	}
	return cases
}

type BoxTest struct {
	HTML                string            `yaml:"html"`
	Style               string            `yaml:"style"`
	EnableDefaultStyles bool              `yaml:"enable_default_styles"`
	Calc                map[string][4]int `yaml:"calc"`

	// ID -> Property（大写开头的） -> 期望值
	Computed map[string]map[string]expectedStyleValue `yaml:"computed"`
}

func TestCalc(t *testing.T) {
	cases := loadTestCases[BoxTest](`testdata/box.yaml`)
	for i, tc := range cases {
		fontManager := NewFontManager()
		imageManager := NewImageManager()

		// 早期非标准页面兼容
		if strings.HasPrefix(tc.HTML, `<block`) || strings.HasPrefix(tc.HTML, `<inline`) {
			updated := `<document><style>` +
				tc.Style +
				`</style>` +
				tc.HTML +
				`</document>`
			tc.HTML = updated
		}

		doc := _NewDocument(
			1024, 768,
			fstest.MapFS{
				`main.html`: &fstest.MapFile{
					Data: []byte(tc.HTML),
					Mode: 0644,
				},
			},
			fontManager, imageManager,
		)
		if err := doc.load(`main.html`); err != nil {
			t.Errorf(`文档解析失败：#%d: %v`, i, err)
			continue
		}

		// 清空默认样式方便测试。
		if !tc.EnableDefaultStyles {
			doc.defaultStyles = Styles{}
		}

		doc.layout()

		for id, rect := range tc.Calc {
			box := doc.GetBoxByID[Box](id)
			if box == nil {
				panic(`指定编号的盒子没找到：` + id)
			}
			got := [4]int{
				box.Base().layoutBox.X,
				box.Base().layoutBox.Y,
				box.Base().layoutBox.Width,
				box.Base().layoutBox.Height,
			}
			if got[0] != rect[0] || got[1] != rect[1] || got[2] != rect[2] || got[3] != rect[3] {
				t.Errorf(`排版错误：#%d, id: %s, want: %v -> got: %v`, i, id, rect, got)
			}
		}
		for id, styles := range tc.Computed {
			box := doc.GetBoxByID[Box](id)
			if box == nil {
				panic(`指定编号的盒子没找到：` + id)
			}
			computedStylesValue := reflect.ValueOf(box.Base().computedStyles)
			for name, expected := range styles {
				field := computedStylesValue.FieldByName(name)
				if !field.IsValid() {
					panic(`找不到字段：` + name)
				}
				fieldValue := field.Interface()
				expectedValue := expected.value
				if fieldValue != expectedValue {
					t.Errorf("样式错误：#%d, id: %s, name: %s\nwant: %+v\ngot:  %+v",
						i, id, name, expectedValue, fieldValue)
				}
			}
		}
	}
}

type testMetricsFace struct {
	font.Face
	metrics font.Metrics
}

func (f testMetricsFace) Metrics() font.Metrics {
	return f.metrics
}

func TestSegmentInlineStopsWhenFirstCharacterDoesNotFit(t *testing.T) {
	fontManager := NewFontManager()
	fontManager.faces[_FontFaceKey{Family: `system`, Size: 32}] = &FontFace{
		Face:  basicfont.Face7x13,
		cache: map[rune]GlyphValue{},
	}
	doc := _NewDocument(100, 100, nil, fontManager, nil)
	text := NewText(doc)
	text.computedStyles = Styles{}
	text.computedStyles.SetFontFamily(`system`)
	text.computedStyles.SetFontSize(NumberLength(32))
	text.SetText(`A`)

	if more := text.SegmentInline(1, 100); more {
		t.Fatal(`SegmentInline returned more=true without consuming a character`)
	}
	if got := text.textRunDataIndex; got != 0 {
		t.Fatalf(`textRunDataIndex = %d, want 0`, got)
	}
}

func TestSegmentBlockKeepsLineHeightWhenAvailableHeightIsSmaller(t *testing.T) {
	fontManager := NewFontManager()
	fontManager.faces[_FontFaceKey{Family: `system`, Size: 32}] = &FontFace{
		Face: testMetricsFace{
			Face: basicfont.Face7x13,
			metrics: font.Metrics{
				Ascent:  fixed.I(30),
				Descent: fixed.I(8),
			},
		},
		cache: map[rune]GlyphValue{},
	}
	doc := _NewDocument(100, 30, nil, fontManager, nil)
	text := NewText(doc)
	text.computedStyles = Styles{}
	text.computedStyles.SetFontFamily(`system`)
	text.computedStyles.SetFontSize(NumberLength(32))
	text.SetText(`A`)

	text.SegmentBlock(100, 30)

	if got, want := text.layoutBox.Height, 38; got != want {
		t.Fatalf(`text height = %d, want line height %d`, got, want)
	}
}

func TestShouldDrawTextLine(t *testing.T) {
	tests := []struct {
		name             string
		lineCount        int
		usedHeight       int
		lineHeight       int
		contentMaxHeight int
		want             bool
	}{
		{name: `single line may overflow`, lineCount: 1, lineHeight: 20, contentMaxHeight: 10, want: true},
		{name: `complete multiline line`, lineCount: 2, usedHeight: 20, lineHeight: 20, contentMaxHeight: 40, want: true},
		{name: `incomplete multiline line`, lineCount: 2, usedHeight: 20, lineHeight: 20, contentMaxHeight: 39, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldDrawTextLine(tt.lineCount, tt.usedHeight, tt.lineHeight, tt.contentMaxHeight)
			if got != tt.want {
				t.Fatalf(`shouldDrawTextLine() = %t, want %t`, got, tt.want)
			}
		})
	}
}

func TestTextLineWidthIncludesAllFragments(t *testing.T) {
	line := _TextLine{
		Fragments: []_TextRunFragment{
			{layoutBox: Rect{Width: 20}},
			{layoutBox: Rect{Width: 30}},
		},
	}

	if got, want := line.Width(), 50; got != want {
		t.Fatalf(`line.Width() = %d, want %d`, got, want)
	}
}

func TestTextLineHorizontalOffset(t *testing.T) {
	line := _TextLine{Fragments: []_TextRunFragment{{layoutBox: Rect{Width: 40}}}}

	tests := []struct {
		align string
		want  int
	}{
		{align: ``, want: 0},
		{align: `middle`, want: 0},
		{align: `center`, want: 30},
		{align: `both`, want: 30},
	}
	for _, tt := range tests {
		if got := line.horizontalOffset(100, tt.align); got != tt.want {
			t.Errorf(`horizontalOffset(100, %q) = %d, want %d`, tt.align, got, tt.want)
		}
	}
}

func TestScrollMaxRowsHeight(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  int
	}{
		{name: `empty`, count: 0, want: 0},
		{name: `one row`, count: 1, want: 20},
		{name: `partial second row`, count: 3, want: 45},
		{name: `maximum rows`, count: 6, want: 70},
		{name: `more than maximum rows`, count: 20, want: 70},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scroll := NewScroll(nil)
			scroll.rows = 3
			scroll.cols = 2
			scroll.computedStyles.SetGap(5)
			scroll.rowHeight = 20
			scroll.shrinkRows = true
			scroll.count = tt.count

			scroll.Calc(100, 100, Constraints{})
			if got := scroll.layoutBox.Height; got != tt.want {
				t.Errorf(`height = %d, want %d`, got, tt.want)
			}
		})
	}
}

func TestScrollWidthConstraintControlsItemSizing(t *testing.T) {
	newScroll := func() (*Scroll, *Block) {
		doc := _NewDocument(100, 40, nil, nil, nil)
		scroll := NewScroll(doc)
		scroll._EventTarget.box = scroll
		scroll.cols = 1
		scroll.rows = 1

		var item *Block
		scroll._setItems(1, func() (Box, any) {
			item = NewBlock(doc)
			item._EventTarget.box = item
			item.inlineStyles.SetWidth(NumberLength(30))
			return item, nil
		}, func(any, int) {})
		return scroll, item
	}

	t.Run(`shrink to item width`, func(t *testing.T) {
		scroll, item := newScroll()
		scroll.Calc(100, 40, Constraints{})
		if got, want := scroll.layoutBox.Width, 30; got != want {
			t.Fatalf(`scroll width = %d, want %d`, got, want)
		}
		if got, want := item.layoutBox.Width, 30; got != want {
			t.Fatalf(`item width = %d, want %d`, got, want)
		}
	})

	t.Run(`fill available width`, func(t *testing.T) {
		scroll, _ := newScroll()
		scroll.Calc(100, 40, Constraints{PrefersMaxWidth: true})
		if got, want := scroll.layoutBox.Width, 100; got != want {
			t.Fatalf(`scroll width = %d, want %d`, got, want)
		}
	})
}

func TestScrollRowsKeepsFixedHeight(t *testing.T) {
	scroll := NewScroll(nil)
	scroll.rows = 3
	scroll.cols = 1
	scroll.computedStyles.SetGap(5)
	scroll.count = 1

	scroll.Calc(100, 100, Constraints{PrefersMaxHeight: true})
	if got, want := scroll.layoutBox.Height, 100; got != want {
		t.Errorf(`height = %d, want %d`, got, want)
	}
}

func TestScrollMaxRowsUsesFullHeightAsLimit(t *testing.T) {
	doc := _NewDocument(100, 100, nil, nil, nil)
	scroll := NewScroll(doc)
	doc.root = scroll
	if err := scroll.SetProp(`max-rows`, `3`); err != nil {
		t.Fatal(err)
	}
	if err := scroll.SetProp(`gap`, `5`); err != nil {
		t.Fatal(err)
	}
	scroll.count = 1

	scroll.Calc(100, 100, Constraints{PrefersMaxHeight: true})
	if got, want := scroll.layoutBox.Height, 30; got != want {
		t.Errorf(`height = %d, want %d`, got, want)
	}
}

func TestScrollGapUsesStyles(t *testing.T) {
	for _, attr := range []string{``, `gap="6"`} {
		t.Run(attr, func(t *testing.T) {
			doc := newFlexTestDocument(t, `<style>scroll { gap: 4; } scroll.wide { gap: 8; }</style><block><scroll id="list" rows="2" cols="2" width="100" height="100" `+attr+`></scroll></block>`, 100, 100)
			scroll := doc.GetBoxByID[*Scroll](`list`)
			scroll._setItems(4, func() (Box, any) {
				box := NewBlock(doc)
				box._EventTarget.box = box
				return box, nil
			}, func(any, int) {})
			check := func(gap int) {
				t.Helper()
				doc.layout()
				if got := scroll.GetComputedStyles().Gap; got != gap {
					t.Fatalf("computed gap = %d, want %d", got, gap)
				}
				size := (100 - gap) / 2
				for i, child := range scroll.Children() {
					want := Rect{(i % 2) * (size + gap), (i / 2) * (size + gap), size, size}
					if got := child.GetLayoutBox(); got != want {
						t.Fatalf("slot %d: got %+v, want %+v", i, got, want)
					}
					if child.GetComputedStyles().Gap != 0 || child.Children()[0].GetComputedStyles().Gap != 0 {
						t.Fatal("gap inherited by scroll contents")
					}
				}
			}
			initial := 4
			if attr != `` {
				initial = 6
			}
			check(initial)
			doc.layoutDirty = false
			scroll.ClassAdd(`wide`)
			if !doc.layoutDirty {
				t.Fatal("class change did not invalidate layout")
			}
			if attr == `` {
				check(8)
			} else {
				check(6)
			} // 内联属性优先于 CSS。
			for _, gap := range []int{12, 0} {
				doc.layoutDirty = false
				if err := scroll.SetProp(`gap`, strconv.Itoa(gap)); err != nil {
					t.Fatal(err)
				}
				if !doc.layoutDirty {
					t.Fatal("gap change did not invalidate layout")
				}
				check(gap)
			}
			for _, invalid := range []string{`-1`, `1.5`, `bad`} {
				if err := scroll.SetProp(`gap`, invalid); err == nil {
					t.Fatalf("accepted invalid gap %q", invalid)
				}
				check(0)
			}
		})
	}
}

func TestScrollStateNavigate(t *testing.T) {
	tests := []struct {
		name    string
		state   _ScrollState
		key     KeyName
		want    _ScrollState
		changed bool
	}{
		{
			name:    `down selects the first item`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: -1, colIndex: 0},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 0},
			changed: true,
		},
		{
			name:    `down moves to the next visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1},
			changed: true,
		},
		{
			name:    `down scrolls past the last visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 0},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 0, itemOffset: 3},
			changed: true,
		},
		{
			name:    `down adjusts the column for a partial last row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 2},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			changed: true,
		},
		{
			name:    `down stops at the last data row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			changed: false,
		},
		{
			name:    `up moves to the previous visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 1},
			key:     Up,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: true,
		},
		{
			name:    `up scrolls before the first visible row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1, itemOffset: 3},
			key:     Up,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: true,
		},
		{
			name:    `up stops at the first data row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     Up,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: false,
		},
		{
			name:    `left moves to the previous column`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 2},
			key:     Left,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: true,
		},
		{
			name:    `right moves to the next column`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     Right,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 2},
			changed: true,
		},
		{
			name:    `right stops at the end of a partial last row`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			key:     Right,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 1, colIndex: 1, itemOffset: 3},
			changed: false,
		},
		{
			name:    `left pages a single column and keeps the selected row`,
			state:   _ScrollState{count: 10, rows: 3, cols: 1, rowIndex: 1, colIndex: 0, itemOffset: 3},
			key:     Left,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 1, colIndex: 0},
			changed: true,
		},
		{
			name:    `left selects the first item when a full page is unavailable`,
			state:   _ScrollState{count: 10, rows: 3, cols: 1, rowIndex: 2, colIndex: 0, itemOffset: 1},
			key:     Left,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 0, colIndex: 0},
			changed: true,
		},
		{
			name:    `right pages a single column and keeps the selected row`,
			state:   _ScrollState{count: 10, rows: 3, cols: 1, rowIndex: 1, colIndex: 0},
			key:     Right,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 1, colIndex: 0, itemOffset: 3},
			changed: true,
		},
		{
			name:    `right selects the last item when a full page is unavailable`,
			state:   _ScrollState{count: 8, rows: 3, cols: 1, rowIndex: 2, colIndex: 0, itemOffset: 3},
			key:     Right,
			want:    _ScrollState{rows: 3, cols: 1, rowIndex: 2, colIndex: 0, itemOffset: 5},
			changed: true,
		},
		{
			name:    `navigation does nothing when the list is empty`,
			state:   _ScrollState{count: 0, rows: 2, cols: 3, rowIndex: -1, colIndex: 0},
			key:     Down,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: -1, colIndex: 0},
			changed: false,
		},
		{
			name:    `non-navigation keys are ignored`,
			state:   _ScrollState{count: 8, rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			key:     A,
			want:    _ScrollState{rows: 2, cols: 3, rowIndex: 0, colIndex: 1},
			changed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state
			want := tt.want
			want.count = tt.state.count
			changed := got.navigate(tt.key)
			if got != want {
				t.Errorf(`state = %+v, want %+v`, got, want)
			}
			if changed != tt.changed {
				t.Errorf(`changed = %t, want %t`, changed, tt.changed)
			}
		})
	}
}
