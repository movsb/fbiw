package fbiw

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/goccy/go-yaml"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/goregular"
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

func TestFlexDemoLayout(t *testing.T) {
	fm := NewFontManager()
	defer fm.Close()
	if err := fm.AddFont(fstest.MapFS{`regular.ttf`: &fstest.MapFile{Data: goregular.TTF}}, `regular.ttf`, `system`, false, false); err != nil {
		t.Fatal(err)
	}
	doc := _NewDocument(1024, 768, os.DirFS(`demo/flex`), fm, nil)
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		doc.layout()
		walkBox(doc.root, func(box Box) bool {
			if !displaying(box) {
				return false
			}
			r := box.GetLayoutBox()
			if r.Width < 0 || r.Height < 0 {
				t.Errorf("negative size for %s#%s: %+v", box.Base().Tag, box.Base().ID, r)
			}
			if parent := box.Parent(); parent != nil {
				p := parent.GetLayoutBox()
				if r.X < 0 || r.Y < 0 || r.X+r.Width > p.Width || r.Y+r.Height > p.Height {
					t.Errorf("%s#%s overflows: %+v inside %+v", box.Base().Tag, box.Base().ID, r, p)
				}
			}
			return true
		})
		doc.paint(NewCanvas(1024, 768))
	}
	check()
	set := func(id, name, value string) {
		t.Helper()
		if err := doc.GetBoxByID[Box](id).SetProp(name, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, grow := range []string{`1`, `5`, `2`} {
		set(`grow-middle`, `flex-grow`, grow)
		check()
	}
	for _, justify := range []string{`start`, `center`, `end`, `space-between`, `space-around`, `space-evenly`} {
		set(`alignment`, `justify-content`, justify)
		for _, align := range []string{`start`, `center`, `end`, `stretch`} {
			set(`alignment`, `align-items`, align)
			check()
		}
	}
	set(`alignment-last`, `display`, `false`)
	check()
	set(`alignment-last`, `display`, `true`)
	check()
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
	for _, tag := range []string{`toggle`, `check`, `progress`, `select`, `img`, `scroll`} {
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
		"check":    func(d *Document) Box { return NewCheckBox(d) },
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
			doc.computedStyles = Styles{}
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

type richTextStringer int

func (value richTextStringer) String() string {
	return fmt.Sprintf(`value-%d`, value)
}

func TestSetRichSupportsNumberedArguments(t *testing.T) {
	doc := &Document{}
	box, err := parseBox(doc, strings.NewReader(`<text>old</text>`))
	if err != nil {
		t.Fatal(err)
	}
	text := box.(*Text)
	text.textDrawLineOffset = 3
	text.marquee.offset = 12
	text.marquee.direction = -1
	doc.layoutDirty = false

	args := make([]any, 10)
	for index := range args {
		args[index] = index + 1
	}
	args[0] = richTextStringer(7)
	if err := text.SetRich(`<b>{$10}</b>:<i>{$1}</i>:{$1}:{$2}:{$3}:{$4}:{$5}:{$6}:{$7}:{$8}:{$9}`, args...); err != nil {
		t.Fatal(err)
	}

	if got, want := text.GetText(), `10:value-7:value-7:2:3:4:5:6:7:8:9`; got != want {
		t.Fatalf(`GetText() = %q, want %q`, got, want)
	}
	if len(text.children) != 2 {
		t.Fatalf(`rich children = %d, want 2`, len(text.children))
	}
	if _, ok := text.children[0].(*BoldText); !ok {
		t.Fatalf(`first rich child = %T, want *BoldText`, text.children[0])
	}
	if _, ok := text.children[1].(*ItalicText); !ok {
		t.Fatalf(`second rich child = %T, want *ItalicText`, text.children[1])
	}
	for _, child := range text.children {
		if child.Parent() != text.Base() {
			t.Fatalf(`rich child parent = %T, want target text`, child.Parent())
		}
	}
	if !text.children[0].GetComputedStyles().FontBold || !text.children[1].GetComputedStyles().FontItalic {
		t.Fatalf(`rich styles not applied: bold=%t italic=%t`, text.children[0].GetComputedStyles().FontBold, text.children[1].GetComputedStyles().FontItalic)
	}
	if text.textDrawLineOffset != 0 || text.marquee.offset != 0 || text.marquee.direction != 1 {
		t.Fatal(`successful SetRich() did not reset scrolling state`)
	}
	if !doc.layoutDirty {
		t.Fatal(`successful SetRich() did not request layout`)
	}
}

func TestSetRichEscapesArgumentsAndPreservesOrdinaryBraces(t *testing.T) {
	doc := &Document{}
	text := NewText(doc)
	if err := text.SetRich(`{ordinary} {{$1} <b>{$1}</b> {$2} {$3}`, `<i>&"'</i>`, true, 42); err != nil {
		t.Fatal(err)
	}

	if got, want := text.GetText(), `{ordinary} {$1}<i>&"'</i>true 42`; got != want {
		t.Fatalf(`GetText() = %q, want %q`, got, want)
	}
	if len(text.children) != 1 {
		t.Fatalf(`rich children = %d, want only the literal template b element`, len(text.children))
	}
	if _, ok := text.children[0].(*BoldText); !ok {
		t.Fatalf(`rich child = %T, want *BoldText`, text.children[0])
	}
}

func TestSetRichRejectsInvalidInputAtomically(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		args []any
	}{
		{name: `zero index`, tmpl: `{$0}`},
		{name: `missing index`, tmpl: `{$}`, args: []any{1}},
		{name: `invalid index`, tmpl: `{$x}`, args: []any{1}},
		{name: `unclosed`, tmpl: `{$1`, args: []any{1}},
		{name: `out of range`, tmpl: `{$2}`, args: []any{1}},
		{name: `unused argument`, tmpl: `{$1}`, args: []any{1, 2}},
		{name: `unsupported element`, tmpl: `<br>`, args: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &Document{}
			box, err := parseBox(doc, strings.NewReader(`<text>old<b>content</b></text>`))
			if err != nil {
				t.Fatal(err)
			}
			text := box.(*Text)
			oldChild := text.children[0]
			oldParent := oldChild.Parent()
			text.textDrawLineOffset = 3
			text.marquee.offset = 12
			text.marquee.direction = -1

			if err := text.SetRich(tt.tmpl, tt.args...); err == nil {
				t.Fatal(`SetRich() returned nil error`)
			}
			if got := text.GetText(); got != `oldcontent` {
				t.Fatalf(`GetText() after error = %q, want old content`, got)
			}
			if len(text.children) != 1 || text.children[0] != oldChild || oldChild.Parent() != oldParent {
				t.Fatal(`SetRich() changed children after error`)
			}
			if text.textDrawLineOffset != 3 || text.marquee.offset != 12 || text.marquee.direction != -1 {
				t.Fatal(`SetRich() changed scrolling state after error`)
			}
		})
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

func TestHorizontalMarqueeDoesNotWrap(t *testing.T) {
	doc := newFlexTestDocument(t, `<block><text id="text" width="30" marquee="horizontal">ABCDEFGHIJKL</text></block>`, 100, 100)
	text := doc.GetBoxByID[*Text](`text`)
	if len(text.textLines) != 1 {
		t.Fatalf(`horizontal marquee lines = %d, want 1`, len(text.textLines))
	}
	if text.textLineMaxWidth <= text.layoutBox.Width-text.HorizontalInsets() {
		t.Fatalf(`text width = %d, content width = %d; want overflow`, text.textLineMaxWidth, text.layoutBox.Width-text.HorizontalInsets())
	}
}

func TestHorizontalMarqueeUsesParentWidth(t *testing.T) {
	doc := newFlexTestDocument(t, `<block width="30"><text id="text" marquee="horizontal">ABCDEFGHIJKL</text></block>`, 100, 100)
	text := doc.GetBoxByID[*Text](`text`)
	if text.layoutBox.Width != 30 {
		t.Fatalf(`horizontal marquee width = %d, want parent width 30`, text.layoutBox.Width)
	}
	if text.textLineMaxWidth <= text.layoutBox.Width {
		t.Fatalf(`text width = %d, box width = %d; want overflow`, text.textLineMaxWidth, text.layoutBox.Width)
	}
}

func TestHorizontalMarqueeInsideInlineUsesRemainingWidth(t *testing.T) {
	doc := newFlexTestDocument(t, `<block width="50"><inline spacer><text id="text" marquee="horizontal">ABCDEFGHIJKL</text></inline></block>`, 100, 100)
	text := doc.GetBoxByID[*Text](`text`)
	if text.layoutBox.Width != 50 {
		t.Fatalf(`inline horizontal marquee width = %d, want 50`, text.layoutBox.Width)
	}
	if len(text.textLines) != 1 || text.textLineMaxWidth <= text.layoutBox.Width {
		t.Fatalf(`lines = %d, text width = %d, box width = %d; want single overflowing line`, len(text.textLines), text.textLineMaxWidth, text.layoutBox.Width)
	}
}

func TestStoppingMarqueeResetsPosition(t *testing.T) {
	doc := &Document{}
	text := NewText(doc)
	text.marquee.offset = 17.5
	text.marquee.direction = -1
	canceled := false
	text.marquee.cancel = func() { canceled = true }

	text.SetMarqueeRunning(false)

	if !canceled || text.MarqueeRunning() || text.marquee.offset != 0 || text.marquee.direction != 1 {
		t.Fatalf(`stopped marquee = %+v, canceled=%t`, text.marquee, canceled)
	}
}

func TestMarqueeCountStopsAfterRoundTrip(t *testing.T) {
	app, doc, clock := newAnimationTestApp(t)
	text := NewText(doc)
	text.marquee.axis = `horizontal`
	text.marquee.pause = 0
	text.marquee.count = 1
	text.layoutBox = Rect{Width: 10, Height: 13}
	text.textLineMaxWidth = 20
	text.textLines = []_TextLine{{MaxHeight: 13}}

	text.updateMarquee()
	clock.now = clock.now.Add(animationFrameInterval)
	animationStep(app)
	clock.now = clock.now.Add(time.Second)
	animationStep(app)
	if text.marquee.offset != 10 || text.marquee.direction != -1 {
		t.Fatalf(`marquee at end: offset=%v direction=%v`, text.marquee.offset, text.marquee.direction)
	}
	clock.now = clock.now.Add(time.Second)
	animationStep(app)
	if text.marquee.offset != 0 || text.marquee.completed != 1 || text.marquee.cancel != nil {
		t.Fatalf(`completed marquee = %+v`, text.marquee)
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

type scrollSelectionAwareTestItem struct {
	events []bool
	bound  []int
}

func (i *scrollSelectionAwareTestItem) ScrollSelectionChanged(selected bool) {
	i.events = append(i.events, selected)
}

func TestScrollSelectionAwareAndVirtualRebind(t *testing.T) {
	doc := &Document{}
	scroll := NewScroll(doc)
	scroll._EventTarget.box = scroll
	scroll.rows = 2
	items := []*scrollSelectionAwareTestItem{}
	scroll._setItems(3,
		func() (Box, any) {
			item := &scrollSelectionAwareTestItem{}
			items = append(items, item)
			root := NewBlock(doc)
			root._EventTarget.box = root
			return root, item
		},
		func(user any, index int) {
			item := user.(*scrollSelectionAwareTestItem)
			item.bound = append(item.bound, index)
		},
	)

	scroll.SetIndex(0, 0, 0)
	scroll.navigate(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: Down}})
	scroll.navigate(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: Down}})
	// 同一数据索引的重复布局不得再次 bind，否则 SetText 等绑定逻辑会
	// 意外重置列表项内部的动画状态。
	scroll.children[1].(*_ScrollChild).bindData()
	scroll.Deselect()

	if !slices.Equal(items[0].events, []bool{false, true, false}) {
		t.Fatalf(`first item selection events = %v`, items[0].events)
	}
	if !slices.Equal(items[1].events, []bool{false, true, false, true, false}) {
		t.Fatalf(`reused item selection events = %v`, items[1].events)
	}
	if !slices.Equal(items[1].bound, []int{1, 2}) {
		t.Fatalf(`reused item bindings = %v`, items[1].bound)
	}
}

func TestScrollChildClipsOverflowingContent(t *testing.T) {
	doc := &Document{}
	wrapper := _NewScrollChild(doc)
	wrapper.layoutBox = Rect{Width: 10, Height: 6}
	child := NewBlock(doc)
	child._EventTarget.box = child
	child.layoutBox = Rect{Width: 20, Height: 6}
	wrapper.AppendChild(child)
	child.computedStyles.SetBackgroundColor(ColorFromRGBA(255, 255, 255, 255))

	canvas := NewCanvas(20, 6)
	wrapper.Draw(canvas)
	for x := 0; x < 20; x++ {
		painted := canvas.buffer[x*4+3] != 0
		if painted != (x < 10) {
			t.Fatalf(`pixel x=%d painted=%t, want %t`, x, painted, x < 10)
		}
	}
}

func TestRotationTimeline(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	img := NewImage(doc)
	img.SetRotation(30)
	stop := img.Rotate(RotationOptions{Duration: time.Second, Iterations: 2})
	f.now = f.now.Add(250 * time.Millisecond)
	animationStep(app)
	if img.rotation != 120 {
		t.Fatalf("quarter turn: %v", img.rotation)
	}
	stop()
	stop()
	f.now = f.now.Add(time.Second)
	animationStep(app)
	if img.rotation != 120 {
		t.Fatal("cancel changed angle")
	}
	old := img.Rotate(RotationOptions{Duration: time.Second})
	img.Rotate(RotationOptions{Duration: 2 * time.Second, Iterations: 1})
	old()
	f.now = f.now.Add(time.Second)
	animationStep(app)
	if img.rotation != 300 {
		t.Fatalf("restart speed: %v", img.rotation)
	}
	f.now = f.now.Add(5 * time.Second)
	animationStep(app)
	if img.rotation != 120 || len(doc.timeline.animations) != 0 {
		t.Fatal("finite loop failed")
	}
	img.Rotate(RotationOptions{Duration: time.Second})
	f.now = f.now.Add(100*time.Second + 250*time.Millisecond)
	animationStep(app)
	if img.rotation != 210 {
		t.Fatal("infinite loop lost elapsed time")
	}
	doc.Close()
	if !doc.timeline.closed {
		t.Fatal("close did not clean timeline")
	}
}

func TestRotationInvalidOptions(t *testing.T) {
	_, doc, _ := newAnimationTestApp(t)
	img := NewImage(doc)
	for _, fn := range []func(){
		func() { img.SetRotation(math.NaN()) }, func() { img.SetRotation(math.Inf(1)) },
		func() { img.Rotate(RotationOptions{}) },
		func() { img.Rotate(RotationOptions{Duration: time.Second, Iterations: -1}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid value accepted")
				}
			}()
			fn()
		}()
	}
}

func TestRotatedRectangleAndClip(t *testing.T) {
	img := DecodedImage{Width: 3, Height: 1, Pixels: []byte{0, 0, 255, 255, 0, 255, 0, 255, 255, 0, 0, 255}}
	c := NewCanvas(7, 7)
	c.Offset(2, 3).DrawImageRotated(img, 90)
	for y, want := range [][]byte{{0, 0, 255, 255}, {0, 255, 0, 255}, {255, 0, 0, 255}} {
		got := c.buffer[((y+2)*7+3)*4:][:4]
		if !bytes.Equal(got, want) {
			t.Fatalf("pixel %d: %v", y, got)
		}
	}
	clipped := NewCanvas(7, 7)
	clipped.Clip(3, 3, 1, 1).Offset(2, 3).DrawImageRotated(img, 90)
	for y := 0; y < 7; y++ {
		for x := 0; x < 7; x++ {
			if (x != 3 || y != 3) && !bytes.Equal(clipped.buffer[(y*7+x)*4:][:4], make([]byte, 4)) {
				t.Fatal("escaped clip")
			}
		}
	}
	zero := NewCanvas(7, 7)
	plain := NewCanvas(7, 7)
	zero.Offset(2, 3).DrawImageRotated(img, 360)
	plain.Offset(2, 3).DrawImage(img)
	if !bytes.Equal(zero.buffer, plain.buffer) {
		t.Fatal("zero path differs")
	}
}

func TestRotatedTransparentSampling(t *testing.T) {
	// 隐藏的蓝色不能污染半透明红色的插值。
	img := DecodedImage{Width: 2, Height: 1, Pixels: []byte{0, 0, 255, 128, 255, 0, 0, 0}}
	c := NewCanvas(5, 5)
	c.Offset(1, 2).DrawImageRotated(img, 45)
	found := false
	for i := 0; i < len(c.buffer); i += 4 {
		if c.buffer[i] != 0 {
			t.Fatal("transparent color leaked")
		}
		if c.buffer[i+2] > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("missing visible pixels")
	}
}

func BenchmarkDrawImageRotated(b *testing.B) {
	for _, size := range []int{128, 256, 512} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			img := DecodedImage{Width: size, Height: size, Pixels: make([]byte, size*size*4)}
			for i := range img.Pixels {
				img.Pixels[i] = 255
			}
			c := NewCanvas(size, size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.DrawImageRotated(img, 37)
			}
		})
	}
}

func TestRotationBackgroundAndFiniteBoundary(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	bg := &Desktop{app: app}
	app.desktops.PushBack(bg)
	addDesktopTestDocument(app, bg)
	img := NewImage(doc)
	img.Rotate(RotationOptions{Duration: time.Second, Iterations: 2})
	f.now = f.now.Add(time.Second)
	animationStep(app)
	if len(doc.timeline.animations) != 1 || img.rotation != 0 {
		t.Fatal("completed before second turn")
	}
	app.SwitchTo(bg)
	f.now = f.now.Add(750 * time.Millisecond)
	animationStep(app)
	if img.rotation != 0 {
		t.Fatal("background updated")
	}
	app.SwitchTo(doc.desktop)
	animationStep(app)
	if img.rotation != 270 {
		t.Fatalf("resume: %v", img.rotation)
	}
	f.now = f.now.Add(250 * time.Millisecond)
	animationStep(app)
	if len(doc.timeline.animations) != 0 || img.rotation != 0 {
		t.Fatal("second turn did not finish")
	}
}

func TestImageRotationClipsAndKeepsLayout(t *testing.T) {
	_, doc, _ := newAnimationTestApp(t)
	img := NewImage(doc)
	img.status = imageLoadStatusScaled
	img.decodedImage = DecodedImage{Width: 1, Height: 1, Pixels: []byte{0, 0, 255, 255}}
	img.layoutBox.Width = 3
	img.layoutBox.Height = 3
	original := img.layoutBox
	img.SetRotation(90)
	c := NewCanvas(5, 5)
	img.Draw(c.Offset(1, 1))
	if c.buffer[(2*5+2)*4+2] != 255 {
		t.Fatal("center moved")
	}
	if img.layoutBox != original {
		t.Fatal("rotation changed layout")
	}
	offscreen := NewCanvas(5, 5)
	img.Draw(offscreen.Clip(0, 0, 1, 1).Offset(2, 2))
	if !bytes.Equal(offscreen.buffer, make([]byte, len(offscreen.buffer))) {
		t.Fatal("empty intersection escaped clip")
	}
}

func TestImageRotationOverflow(t *testing.T) {
	_, doc, _ := newAnimationTestApp(t)
	img := NewImage(doc)
	img.status = imageLoadStatusScaled
	img.decodedImage = DecodedImage{Width: 5, Height: 5, Pixels: make([]byte, 100)}
	for i := range img.decodedImage.Pixels {
		img.decodedImage.Pixels[i] = 255
	}
	img.layoutBox.Width, img.layoutBox.Height = 5, 5
	original := img.layoutBox
	img.SetRotation(45)
	draw := func(allow bool) *Canvas {
		stop := img.Rotate(RotationOptions{Duration: time.Second, Overflow: allow})
		stop()
		c := NewCanvas(11, 11)
		img.Draw(c.Offset(3, 3))
		return c
	}
	clipped, overflow := draw(false), draw(true)
	if clipped.buffer[(2*11+5)*4+3] != 0 || overflow.buffer[(2*11+5)*4+3] == 0 {
		t.Fatal("overflow toggle failed")
	}
	if img.layoutBox != original {
		t.Fatal("overflow changed layout")
	}
	if !bytes.Equal(clipped.buffer, draw(false).buffer) {
		t.Fatal("disabling overflow did not restore clipping")
	}
	stop := img.Rotate(RotationOptions{Duration: time.Second, Overflow: true})
	stop()
	parent := NewCanvas(11, 11)
	img.Draw(parent.Clip(4, 4, 3, 3).Offset(3, 3))
	for y := 0; y < 11; y++ {
		for x := 0; x < 11; x++ {
			if (x < 4 || x >= 7 || y < 4 || y >= 7) && parent.buffer[(y*11+x)*4+3] != 0 {
				t.Fatal("escaped parent clip")
			}
		}
	}
	// 组件本身在父裁剪范围外，其旋转后的溢出仍可见。
	edge := NewCanvas(11, 11)
	img.Draw(edge.Clip(5, 2, 1, 1).Offset(3, 3))
	if edge.buffer[(2*11+5)*4+3] == 0 {
		t.Fatal("culled visible overflow")
	}
	// 图片大于组件时，零角度也保持溢出策略。
	img.layoutBox.Width, img.layoutBox.Height = 3, 3
	img.SetRotation(0)
	zero := draw(true)
	if zero.buffer[(2*11+2)*4+3] == 0 {
		t.Fatal("zero angle lost overflow")
	}
	// 屏幕边界安全裁剪。
	img.SetRotation(45)
	img.Draw(NewCanvas(2, 2).Offset(-1, -1))
}

func TestRotationDirection(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "clockwise"
		if reverse {
			name = "counterclockwise"
		}
		t.Run(name, func(t *testing.T) {
			app, doc, f := newAnimationTestApp(t)
			img := NewImage(doc)
			img.SetRotation(120)
			stop := img.Rotate(RotationOptions{Duration: time.Second, Iterations: 2, Reverse: reverse})
			f.now = f.now.Add(250 * time.Millisecond)
			animationStep(app)
			want := 210.0
			if reverse {
				want = 30
			}
			if img.rotation != want {
				t.Fatalf("quarter turn = %v, want %v", img.rotation, want)
			}
			f.now = f.now.Add(time.Second)
			animationStep(app)
			if img.rotation != want {
				t.Fatal("loop changed direction")
			}
			f.now = f.now.Add(750 * time.Millisecond)
			animationStep(app)
			if img.rotation != 120 || len(doc.timeline.animations) != 0 {
				t.Fatal("finite rotation failed to finish at initial orientation")
			}
			stop()
			stop()
			// 从当前角度反向重启，不跳变；旧停止函数不影响新动画。
			img.Rotate(RotationOptions{Duration: time.Second, Reverse: !reverse})
			stop()
			if img.rotation != 120 {
				t.Fatal("restart changed angle synchronously")
			}
			f.now = f.now.Add(250 * time.Millisecond)
			animationStep(app)
			want = 30
			if reverse {
				want = 210
			}
			if img.rotation != want {
				t.Fatalf("reverse restart = %v, want %v", img.rotation, want)
			}
		})
	}
}

func TestImageScaleDrawing(t *testing.T) {
	_, doc, _ := newAnimationTestApp(t)
	img := NewImage(doc)
	img.status = imageLoadStatusScaled
	img.decodedImage = DecodedImage{Width: 3, Height: 3, Pixels: make([]byte, 36)}
	for i := range img.decodedImage.Pixels {
		img.decodedImage.Pixels[i] = 255
	}
	img.layoutBox.Width, img.layoutBox.Height = 3, 3
	layout := img.layoutBox
	stop := img.Rotate(RotationOptions{Duration: time.Second, Overflow: true})
	stop()
	img.SetScale(2)
	for _, angle := range []float64{0, 90} {
		img.SetRotation(angle)
		c := NewCanvas(9, 9)
		img.Draw(c.Offset(3, 3))
		if c.buffer[(4*9+4)*4] != 255 {
			t.Fatal("center shifted")
		}
		if c.buffer[(4*9+2)*4] == 0 {
			t.Fatal("scale did not expand drawing")
		}
		parent := NewCanvas(9, 9)
		img.Draw(parent.Clip(3, 3, 3, 3).Offset(3, 3))
		if parent.buffer[(4*9+2)*4] != 0 {
			t.Fatal("scale escaped parent clip")
		}
	}
	if img.layoutBox != layout {
		t.Fatal("scale changed layout")
	}
	stop = img.Rotate(RotationOptions{Duration: time.Second})
	stop()
	c := NewCanvas(9, 9)
	img.Draw(c.Offset(3, 3))
	if c.buffer[(4*9+2)*4] != 0 {
		t.Fatal("scale escaped component clip")
	}
	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		for _, set := range []func(float64){img.SetScale} {
			func() {
				defer func() {
					if recover() == nil {
						t.Error("invalid scale accepted")
					}
				}()
				set(scale)
			}()
		}
	}
}

func TestImageScaleWithPublicAnimation(t *testing.T) {
	app, doc, f := newAnimationTestApp(t)
	img := NewImage(doc)
	img.Activate()
	if img.scale != 1 || doc.timeline != nil {
		t.Fatal("activation started scale animation")
	}
	scale := 1.0
	var cancel func()
	scaleTo := func(target float64) {
		if cancel != nil {
			cancel()
		}
		interpolate := NumberAnimator(scale, target)
		cancel = doc.Animate(AnimationOptions{Duration: 200 * time.Millisecond, Easing: EaseOut,
			OnUpdate: func(progress float64) { scale = interpolate(progress); img.SetScale(scale) },
		})
	}
	scaleTo(1.1)
	f.now = f.now.Add(100 * time.Millisecond)
	animationStep(app)
	if math.Abs(img.scale-1.075) > 1e-12 {
		t.Fatalf("scale = %v", img.scale)
	}
	NewImage(doc).Activate()
	if math.Abs(img.scale-1.075) > 1e-12 {
		t.Fatal("activation changed scale")
	}
	scaleTo(1)
	if math.Abs(img.scale-1.075) > 1e-12 {
		t.Fatal("retarget jumped")
	}
	f.now = f.now.Add(200 * time.Millisecond)
	animationStep(app)
	if img.scale != 1 || len(doc.timeline.animations) != 0 {
		t.Fatal("scale did not restore")
	}
	scaleTo(1.1)
	f.now = f.now.Add(200 * time.Millisecond)
	animationStep(app)
	if img.scale != 1.1 {
		t.Fatal("scale did not reach target")
	}
	scaleTo(1)
	doc.Close()
	if !doc.timeline.closed {
		t.Fatal("close retained animation")
	}
}
