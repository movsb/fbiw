package fbiw

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseStyleNesting(t *testing.T) {
	nested := Must1(ParseStyle(`
		.card, #panel {
			color: red;
			> .title, &.selected {
				width: 10;
				.icon { height: 20; }
			}
			background-color: black;
		}
	`))
	flat := Must1(ParseStyle(`
		.card { color: red; }
		#panel { color: red; }
		.card > .title { width: 10; }
		.card.selected { width: 10; }
		#panel > .title { width: 10; }
		#panel.selected { width: 10; }
		.card > .title .icon { height: 20; }
		.card.selected .icon { height: 20; }
		#panel > .title .icon { height: 20; }
		#panel.selected .icon { height: 20; }
		.card { background-color: black; }
		#panel { background-color: black; }
	`))

	if !reflect.DeepEqual(nested.Rules, flat.Rules) {
		t.Fatalf("nested CSS 展开结果不一致\nwant: %#v\ngot:  %#v", flat.Rules, nested.Rules)
	}
}

func TestParseStyleSelectorLists(t *testing.T) {
	sheet := Must1(ParseStyle(`block, inline { width: 10; }`))
	if got, want := len(sheet.Rules), 2; got != want {
		t.Fatalf(`Rules 数量 = %d，期望 %d`, got, want)
	}
	if got, want := sheet.Rules[0].Selector[0].Tag, `block`; got != want {
		t.Errorf(`第一个选择器 = %q，期望 %q`, got, want)
	}
	if got, want := sheet.Rules[1].Selector[0].Tag, `inline`; got != want {
		t.Errorf(`第二个选择器 = %q，期望 %q`, got, want)
	}
}

func TestParseStyleExplicitAmpersand(t *testing.T) {
	nested := Must1(ParseStyle(`.card { &#main { width: 10; } & > .icon { height: 20; } }`))
	flat := Must1(ParseStyle(`.card#main { width: 10; } .card > .icon { height: 20; }`))
	if !reflect.DeepEqual(nested.Rules, flat.Rules) {
		t.Fatalf("& 选择器展开结果不一致\nwant: %#v\ngot:  %#v", flat.Rules, nested.Rules)
	}
}

func TestParseStyleNestingErrors(t *testing.T) {
	tests := map[string]string{
		`顶层 &`:    `&.selected { color: red; }`,
		`缺少右大括号`:  `.parent { .child { color: red; }`,
		`悬空直接子元素`: `.parent { > { color: red; } }`,
		`非法嵌套选择器`: `.parent { &:selected { color: red; } }`,
	}
	for name, css := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseStyle(css); err == nil {
				t.Fatalf(`ParseStyle(%q) 未返回错误`, css)
			}
		})
	}
}

func TestNestedStyleMatchesFlatStyle(t *testing.T) {
	newTree := func() (*BaseBox, *BaseBox) {
		parent := &BaseBox{Tag: `block`}
		parent.class.Set(`card`)
		parent.class.Set(`selected`)
		child := &BaseBox{Tag: `text`, parent: parent}
		child.class.Set(`title`)
		parent.children = []Box{child}
		return parent, child
	}

	nestedRoot, nestedChild := newTree()
	flatRoot, flatChild := newTree()
	nested := Must1(ParseStyle(`
		.card {
			color: red;
			&.selected { outline-width: 3; }
			> .title { font-size: 18; }
		}
	`))
	flat := Must1(ParseStyle(`
		.card { color: red; }
		.card.selected { outline-width: 3; }
		.card > .title { font-size: 18; }
	`))

	if err := (_Styler{}).Style(nestedRoot, true, nested); err != nil {
		t.Fatalf(`应用 nested CSS 失败：%v`, err)
	}
	if err := (_Styler{}).Style(flatRoot, true, flat); err != nil {
		t.Fatalf(`应用扁平 CSS 失败：%v`, err)
	}
	if got, want := nestedRoot.GetComputedStyles(), flatRoot.GetComputedStyles(); !reflect.DeepEqual(got, want) {
		t.Errorf("root computed styles 不一致\nwant: %#v\ngot:  %#v", want, got)
	}
	if got, want := nestedChild.GetComputedStyles(), flatChild.GetComputedStyles(); !reflect.DeepEqual(got, want) {
		t.Errorf("child computed styles 不一致\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestStylesPaddingShorthand(t *testing.T) {
	tests := []struct {
		raw  string
		want Value
	}{
		{raw: `10`, want: PaddingValue(10, 10, 10, 10)},
		{raw: `10 20`, want: PaddingValue(10, 20, 10, 20)},
		{raw: `10 20 30`, want: PaddingValue(10, 20, 30, 20)},
		{raw: `10 20 30 40`, want: PaddingValue(10, 20, 30, 40)},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			styles := Styles{}
			if _, _, _, err := styles.Set(`padding`, tt.raw); err != nil {
				t.Fatalf(`Set("padding", %q) 返回错误：%v`, tt.raw, err)
			}
			if styles.Padding != tt.want {
				t.Errorf(`Padding = %+v，期望 %+v`, styles.Padding, tt.want)
			}
		})
	}

	for _, raw := range []string{``, `1 2 3 4 5`, `1 -2`, `65536`} {
		t.Run(`invalid_`+raw, func(t *testing.T) {
			styles := Styles{}
			if _, _, _, err := styles.Set(`padding`, raw); err == nil {
				t.Errorf(`Set("padding", %q) 未返回错误`, raw)
			}
		})
	}
}

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
		box.inlineStyles.Padding = PaddingValue(40, 40, 40, 40)

		styler := _Styler{
			defaultStyles: Must1(ParseStyle(`block { width: 10; height: 20; }`)),
		}
		sheet := Must1(ParseStyle(`
			block { width: 20; }
			.featured { width: 30; }
			#target { width: 40; padding: 30; }
		`))

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
		if got.Padding != PaddingValue(40, 40, 40, 40) {
			t.Errorf(`Padding = %+v，期望内联样式的值 %+v`, got.Padding, PaddingValue(40, 40, 40, 40))
		}
	})

	t.Run(`页面样式覆盖 specificity 更高的默认样式`, func(t *testing.T) {
		box := &BaseBox{Tag: `block`, ID: `target`}
		styler := _Styler{
			defaultStyles: Must1(ParseStyle(`#target { color: red; }`)),
		}
		sheet := Must1(ParseStyle(`block { color: blue; }`))

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
		sheet := Must1(ParseStyle(`block { color: blue; }`))

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

		sheet := Must1(ParseStyle(`
			block { font-size: 20; }
			#child { font-size: 150%; }
			#percentage-grandchild { font-size: 50%; }
			#absolute-grandchild { font-size: 12; }
			#great-grandchild { font-size: 200%; }
		`))

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

	// <document>：20
	// └─ 根节点：1.5rem → 30
	//    ├─ 子节点：10
	//    │  └─ 孙节点：2rem → 40（不受父节点 10 影响）
	//    └─ 子节点：0.75rem → 15
	t.Run(`rem font-size 始终根据 document 字号计算`, func(t *testing.T) {
		root := &BaseBox{Tag: `block`}
		absoluteChild := &BaseBox{Tag: `inline`, ID: `absolute-child`, parent: root}
		grandchild := &BaseBox{Tag: `inline`, ID: `rem-grandchild`, parent: absoluteChild}
		fractionalChild := &BaseBox{Tag: `inline`, ID: `fractional-child`, parent: root}
		root.children = []Box{absoluteChild, fractionalChild}
		absoluteChild.children = []Box{grandchild}

		documentStyles := Styles{FontSize: NumberValue(20)}
		styler := _Styler{documentStyles: &documentStyles}
		sheet := Must1(ParseStyle(`
			block { font-size: 1.5rem; }
			#absolute-child { font-size: 10; }
			#rem-grandchild { font-size: 2rem; }
			#fractional-child { font-size: 0.75rem; }
		`))

		if err := styler.Style(root, true, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}

		tests := []struct {
			name string
			box  Box
			want Value
		}{
			{name: `根节点的 1.5rem`, box: root, want: NumberValue(30)},
			{name: `子节点使用绝对字号`, box: absoluteChild, want: NumberValue(10)},
			{name: `孙节点的 2rem 忽略父节点绝对字号`, box: grandchild, want: NumberValue(40)},
			{name: `小数 0.75rem`, box: fractionalChild, want: NumberValue(15)},
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
		sheet := Must1(ParseStyle(`* { width: 12; }`))

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

	t.Run(`四边 padding 参与布局`, func(t *testing.T) {
		root := &BaseBox{Tag: `block`, ID: `root`}
		child := &BaseBox{Tag: `block`, ID: `child`, parent: root}
		root.children = []Box{child}
		sheet := Must1(ParseStyle(`
			#root { width: 200; height: 100; padding: 10 20 30 40; }
			#child { width: 50; height: 20; }
		`))

		if err := (_Styler{}).Style(root, true, sheet); err != nil {
			t.Fatalf(`Style() 返回错误：%v`, err)
		}
		root.Calc(200, 100, Constraints{})

		if got, want := child.layoutBox.X, 40; got != want {
			t.Errorf(`子节点 X = %d，期望左 padding %d`, got, want)
		}
		if got, want := child.layoutBox.Y, 10; got != want {
			t.Errorf(`子节点 Y = %d，期望上 padding %d`, got, want)
		}
		if got, want := root.HorizontalInsets(), 60; got != want {
			t.Errorf(`水平不可用空间 = %d，期望左右 padding 之和 %d`, got, want)
		}
		if got, want := root.VerticalInsets(), 40; got != want {
			t.Errorf(`垂直不可用空间 = %d，期望上下 padding 之和 %d`, got, want)
		}
	})

	t.Run(`样式应用失败时返回错误并停止遍历`, func(t *testing.T) {
		parent := &BaseBox{Tag: `block`}
		bad := &BaseBox{Tag: `bad`, parent: parent}
		unvisited := &BaseBox{Tag: `inline`, parent: parent}
		parent.children = []Box{bad, unvisited}
		sheet := Must1(ParseStyle(`
			block { width: 10; }
			bad { unknown-property: value; }
			inline { width: 20; }
		`))

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

func TestSpecialColors(t *testing.T) {
	transparent := ColorFromRGBA(0, 0, 2, 0)
	if transparent != 0 {
		t.Fatalf("透明颜色未规范化为零：%#x", transparent)
	}
	if transparent.IsNone() || transparent.IsClear() {
		t.Fatal("普通透明颜色不应成为特殊颜色")
	}

	if !ColorValue(ColorNone).Color().IsNone() {
		t.Fatal("none 编码错误")
	}
	if !ColorValue(ColorClear).Color().IsClear() {
		t.Fatal("clear 编码错误")
	}
}
