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
		if err := doc.load(`main.html`, `.`); err != nil {
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
		_ = i
		sheet, err := ParseStyle([]byte(tc.Style))
		if err != nil {
			t.Error(err)
			continue
		}
		for i, r := range tc.Rules {
			if len(r.Declarations) == 0 {
				tc.Rules[i].Declarations = nil
			}
		}
		if !reflect.DeepEqual(tc.Rules, sheet.Rules) {
			t.Errorf("解析不一致：\nwant: %v\ngot:  %v", tc.Rules, sheet.Rules)
			continue
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
