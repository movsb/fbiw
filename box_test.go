package main

import (
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
		if strings.HasPrefix(tc.HTML, `<block`) {
			updated := `<document><style>` +
				tc.Style +
				`</style>` +
				tc.HTML +
				`</document>`
			tc.HTML = updated
		}

		doc := NewDocument(
			fstest.MapFS{
				`main.html`: &fstest.MapFile{
					Data: []byte(tc.HTML),
					Mode: 0644,
				},
			},
			fontManager, imageManager,
		)
		if err := doc.Load(`main.html`, `.`); err != nil {
			t.Errorf(`文档解析失败：#%d: %v`, i, err)
			continue
		}

		// 清空默认样式方便测试。
		if !tc.EnableDefaultStyles {
			doc.defaultStyles = Styles{}
		}

		doc.Resize(1024, 768)
		if err := doc.Style(); err != nil {
			t.Error(err)
			continue
		}
		doc.Layout()

		for id, rect := range tc.Calc {
			box := doc.GetElementByID(id)
			if box == nil {
				panic(`指定编号的盒子没找到：` + id)
			}
			pos := box.Base().calcPos
			if pos.X != rect[0] ||
				pos.Y != rect[1] ||
				pos.Width != rect[2] ||
				pos.Height != rect[3] {
				t.Errorf(`排版错误：#%d, id: %s, want: %v -> got: %v`, i, id, rect, pos)
			}
		}
		for id, styles := range tc.Computed {
			box := doc.GetElementByID(id)
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
