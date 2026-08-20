package fbiw

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

type BoxTest struct {
	HTML                string            `yaml:"html"`
	Style               string            `yaml:"style"`
	EnableDefaultStyles bool              `yaml:"enable_default_styles"`
	Calc                map[string][4]int `yaml:"calc"`

	// ID -> Property（大写开头的） -> Value
	Computed map[string]map[string]Value `yaml:"computed"`
}

func TestCalc(t *testing.T) {
	cases := LoadTestCases[BoxTest](`testdata/box.yaml`)
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
			box := doc.GetBoxByID(id)
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
			box := doc.GetBoxByID(id)
			if box == nil {
				panic(`指定编号的盒子没找到：` + id)
			}
			computedStylesValue := reflect.ValueOf(box.Base().computedStyles)
			for name, expected := range styles {
				field := computedStylesValue.FieldByName(name)
				if !field.IsValid() {
					panic(`找不到字段：` + name)
				}
				fieldValue := field.Interface().(Value)
				if fieldValue != expected {
					t.Errorf("样式错误：#%d, id: %s, name: %s\nwant: %+v\ngot:  %+v",
						i, id, name, expected, fieldValue)
				}
			}
		}
	}
}

type StyleParseTest struct {
	Style string
	Rules []Rule
}

func TestParseStyle(t *testing.T) {
	cases := LoadTestCases[StyleParseTest](`testdata/style.yaml`)
	for i, tc := range cases {
		sheet, err := ParseStyle([]byte(tc.Style))
		if err != nil {
			t.Errorf("#%d, %v", i+1, err)
			continue
		}
		for i, r := range tc.Rules {
			if len(r.Declarations) == 0 {
				tc.Rules[i].Declarations = nil
			}
		}
		if !reflect.DeepEqual(tc.Rules, sheet.Rules) {
			t.Errorf("解析不一致: #%d\nwant: %+v\ngot:  %+v", i+1, tc.Rules, sheet.Rules)
			continue
		}
	}
}

func TestQuery(t *testing.T) {
	type _Test struct {
		Selector string
		HTML     string
		Boxes    []string
	}

	cases := LoadTestCases[_Test](`testdata/query.yaml`)
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

		boxes := doc.QuerySelectorAll(tc.Selector)
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

func BenchmarkDrawString(b *testing.B) {
	b.SkipNow()
	fm := NewFontManager()
	if err := fm.AddFont(os.DirFS(`.`), `fonts/MapleMonoNormalNL-NF-CN-Regular.ttf`, `system`, false, false); err != nil {
		b.Fatal(err)
	}
	face, err := fm.GetFace(`system`, 30, false, false)
	if err != nil {
		b.Fatal(err)
	}
	b.Run(`dev`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringDevice(`想测试一下字符串绘制`, face, ColorValueFromString(`red`).Color, 1024, 768)
		}
	})
	b.Run(`std`, func(b *testing.B) {
		canvas := Canvas{
			buffer: make([]byte, 1024*768*4),
			width:  1024,
			height: 768,
		}
		for b.Loop() {
			canvas.drawStringStd(`想测试一下字符串绘制`, face, ColorValueFromString(`red`).Color, 1024, 768)
		}
	})
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
