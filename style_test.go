package fbiw

import (
	"strings"
	"testing"
)

func TestStylerStyle(t *testing.T) {
	newTree := func() (*BaseBox, *BaseBox) {
		parent := &BaseBox{Tag: `block`}
		child := &BaseBox{Tag: `inline`, parent: parent}
		parent.children = []Box{child}
		return parent, child
	}

	t.Run(`应用默认样式、文档样式和内联样式`, func(t *testing.T) {
		box := &BaseBox{Tag: `block`, ID: `target`}
		box.class.Set(`featured`)
		box.inlineStyles.Padding = NumberValue(40)

		styler := _Styler{
			defaultStyles: Must1(ParseStyle([]byte(`block { width: 10; height: 20; }`))),
		}
		sheet := Must1(ParseStyle([]byte(`
			block { width: 20; }
			.featured { width: 30; }
			#target { width: 40; padding: 30; }
		`)))

		if err := styler.Style(box, true, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}

		got := box.GetComputedStyles()
		if got.Width != NumberValue(40) {
			t.Errorf(`Width = %+v，期望 ID 选择器的值 %+v`, got.Width, NumberValue(40))
		}
		if got.Height != NumberValue(20) {
			t.Errorf(`Height = %+v，期望默认样式的值 %+v`, got.Height, NumberValue(20))
		}
		if got.Padding != NumberValue(40) {
			t.Errorf(`Padding = %+v，期望内联样式的值 %+v`, got.Padding, NumberValue(40))
		}
	})

	t.Run(`页面样式覆盖 specificity 更高的默认样式`, func(t *testing.T) {
		box := &BaseBox{Tag: `block`, ID: `target`}
		styler := _Styler{
			defaultStyles: Must1(ParseStyle([]byte(`#target { color: red; }`))),
		}
		sheet := Must1(ParseStyle([]byte(`block { color: blue; }`)))

		if err := styler.Style(box, true, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}

		if got, want := box.GetComputedStyles().Color, ColorValueFromString(`blue`); got != want {
			t.Errorf(`Color = %+v，期望页面样式覆盖默认样式后得到 %+v`, got, want)
		}
	})

	t.Run(`继承父节点和 document 样式`, func(t *testing.T) {
		parent, child := newTree()
		documentStyles := Styles{
			Color:    ColorValueFromString(`red`),
			FontSize: NumberValue(18),
			Width:    NumberValue(999),
		}
		styler := _Styler{documentStyles: &documentStyles}
		sheet := Must1(ParseStyle([]byte(`block { color: blue; }`)))

		if err := styler.Style(parent, true, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}

		if got := parent.GetComputedStyles().Color; got != ColorValueFromString(`blue`) {
			t.Errorf(`父节点 Color = %+v，期望样式表覆盖后的蓝色`, got)
		}
		if got := child.GetComputedStyles().Color; got != ColorValueFromString(`blue`) {
			t.Errorf(`子节点 Color = %+v，期望继承父节点的蓝色`, got)
		}
		if got := child.GetComputedStyles().FontSize; got != NumberValue(18) {
			t.Errorf(`子节点 FontSize = %+v，期望继承 document 的值 %+v`, got, NumberValue(18))
		}
		if got := child.GetComputedStyles().Width; !got.Empty() {
			t.Errorf(`子节点不应继承 Width，实际为 %+v`, got)
		}
	})

	// 根节点：20
	// └─ 子节点：150% → 30
	//    ├─ 孙节点：50% → 15
	//    └─ 孙节点：12
	//       └─ 曾孙节点：200% → 24
	t.Run(`多层百分比 font-size 根据父节点字号计算`, func(t *testing.T) {
		root := &BaseBox{Tag: `block`}
		child := &BaseBox{Tag: `inline`, ID: `child`, parent: root}
		percentageGrandchild := &BaseBox{Tag: `inline`, ID: `percentage-grandchild`, parent: child}
		absoluteGrandchild := &BaseBox{Tag: `inline`, ID: `absolute-grandchild`, parent: child}
		greatGrandchild := &BaseBox{Tag: `inline`, ID: `great-grandchild`, parent: absoluteGrandchild}
		root.children = []Box{child}
		child.children = []Box{percentageGrandchild, absoluteGrandchild}
		absoluteGrandchild.children = []Box{greatGrandchild}

		sheet := Must1(ParseStyle([]byte(`
			block { font-size: 20; }
			#child { font-size: 150%; }
			#percentage-grandchild { font-size: 50%; }
			#absolute-grandchild { font-size: 12; }
			#great-grandchild { font-size: 200%; }
		`)))

		if err := (_Styler{}).Style(root, true, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}

		tests := []struct {
			name string
			box  Box
			want Value
		}{
			{name: `子节点的 150% 基于根节点的 20`, box: child, want: NumberValue(30)},
			{name: `孙节点的 50% 基于子节点计算后的 30`, box: percentageGrandchild, want: NumberValue(15)},
			{name: `孙节点使用绝对字号`, box: absoluteGrandchild, want: NumberValue(12)},
			{name: `曾孙节点的 200% 基于绝对字号 12`, box: greatGrandchild, want: NumberValue(24)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := tt.box.GetComputedStyles().FontSize; got != tt.want {
					t.Errorf(`FontSize = %+v，期望 %+v`, got, tt.want)
				}
			})
		}
	})

	t.Run(`descendents 为 false 时只处理当前节点`, func(t *testing.T) {
		parent, child := newTree()
		sheet := Must1(ParseStyle([]byte(`* { width: 12; }`)))

		if err := (_Styler{}).Style(parent, false, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}

		if got := parent.GetComputedStyles().Width; got != NumberValue(12) {
			t.Errorf(`父节点 Width = %+v，期望 %+v`, got, NumberValue(12))
		}
		if got := child.GetComputedStyles().Width; !got.Empty() {
			t.Errorf(`子节点不应被处理，Width 实际为 %+v`, got)
		}
	})

	t.Run(`样式应用失败时返回错误并停止遍历`, func(t *testing.T) {
		parent := &BaseBox{Tag: `block`}
		bad := &BaseBox{Tag: `bad`, parent: parent}
		unvisited := &BaseBox{Tag: `inline`, parent: parent}
		parent.children = []Box{bad, unvisited}
		sheet := Must1(ParseStyle([]byte(`
			block { width: 10; }
			bad { unknown-property: value; }
			inline { width: 20; }
		`)))

		err := (_Styler{}).Style(parent, true, sheet)
		if err == nil || !strings.Contains(err.Error(), `未知样式属性`) {
			t.Fatalf(`Style() 错误 = %v，期望包含“未知样式属性”`, err)
		}
		if got := parent.GetComputedStyles().Width; got != NumberValue(10) {
			t.Errorf(`出错前父节点应已完成处理，Width = %+v`, got)
		}
		if got := unvisited.GetComputedStyles().Width; !got.Empty() {
			t.Errorf(`出错后的兄弟节点不应被处理，Width 实际为 %+v`, got)
		}
	})
}
