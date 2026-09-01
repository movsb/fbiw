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
			scroll.gap = 5
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
	scroll.gap = 5
	scroll.count = 1

	scroll.Calc(100, 100, Constraints{PrefersMaxHeight: true})
	if got, want := scroll.layoutBox.Height, 100; got != want {
		t.Errorf(`height = %d, want %d`, got, want)
	}
}

func TestScrollMaxRowsUsesFullHeightAsLimit(t *testing.T) {
	scroll := NewScroll(nil)
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
