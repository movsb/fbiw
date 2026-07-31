package main

import (
	"gofb/style"
	"gofb/utils"
	"reflect"
	"testing"
)

type BoxTest struct {
	HTML                string            `yaml:"html"`
	Style               string            `yaml:"style"`
	EnableDefaultStyles bool              `yaml:"enable_default_styles"`
	Calc                map[string][4]int `yaml:"calc"`

	// ID -> Property（大写开头的） -> Value
	Computed map[string]map[string]style.Value `yaml:"computed"`
}

func getByID(root Box, id string) Box {
	if root.Base().ID == id {
		return root
	}
	for _, child := range root.Base().Children {
		if box := getByID(child, id); box != nil {
			return box
		}
	}
	return nil
}

func TestCalc(t *testing.T) {
	cases := utils.LoadTestCases[BoxTest](`testdata/box.yaml`)
	for i, tc := range cases {
		fontManager := NewFontManager()
		doc := NewDocument(fontManager)
		// 清空默认样式方便测试。
		if !tc.EnableDefaultStyles {
			doc.defaultStyles = style.Styles{}
		}
		if i == 10 {
			i += 0
		}
		root := loadBox(doc, []byte(tc.HTML))
		parsed := utils.Must1(style.ParseStyle([]byte(tc.Style)))
		applyStyles(root, []*style.Sheet{parsed})
		root.Calc(1024, 768)
		for id, rect := range tc.Calc {
			box := getByID(root, id)
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
			box := getByID(root, id)
			if box == nil {
				panic(`指定编号的盒子没找到：` + id)
			}
			computedStylesValue := reflect.ValueOf(box.Base().computedStyles)
			for name, expected := range styles {
				field := computedStylesValue.FieldByName(name)
				if !field.IsValid() {
					panic(`找不到字段：` + name)
				}
				fieldValue := field.Interface().(style.Value)
				if fieldValue != expected {
					t.Errorf("样式错误：#%d, id: %s, name: %s\nwant: %+v\ngot:  %+v",
						i, id, name, expected, fieldValue)
				}
			}
		}
	}
}
