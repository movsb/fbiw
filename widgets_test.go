package fbiw

import (
	"testing"
	"testing/fstest"
)

func newButtonDocument(t *testing.T, markup string) (*Document, *Button) {
	t.Helper()
	doc := _NewDocument(480, 320, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(markup)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	button := doc.QuerySelector[*Button](`button`)
	if button == nil {
		t.Fatal(`找不到 button`)
	}
	return doc, button
}

func TestButtonVariants(t *testing.T) {
	tests := []struct {
		variant ButtonVariant
		class   string
		color   string
	}{
		{ButtonNormal, ``, `#e8eaed`},
		{ButtonPrimary, `button-primary`, `#3358d4`},
		{ButtonDestructive, `button-destructive`, `#ce2c31`},
	}
	for _, test := range tests {
		t.Run(string(test.variant), func(t *testing.T) {
			attribute := ``
			if test.variant != ButtonNormal {
				attribute = ` variant="` + string(test.variant) + `"`
			}
			_, button := newButtonDocument(t, `<document><block><button`+attribute+`><text>按钮</text></button></block></document>`)
			if button.Variant() != test.variant {
				t.Fatalf(`variant 不正确：got=%s want=%s`, button.Variant(), test.variant)
			}
			if test.class != `` && !button.ClassContains(test.class) {
				t.Fatalf(`缺少状态类：%s`, test.class)
			}
			if got, want := button.GetComputedStyles().BackgroundColor.Color, ColorValueFromString(test.color).Color; got != want {
				t.Fatalf(`背景色不正确：got=%v want=%v`, got, want)
			}
		})
	}
}

func TestButtonOnClick(t *testing.T) {
	doc, button := newButtonDocument(t, `<document><block><button><text>按钮</text></button></block></document>`)
	clicks := 0
	button.OnClick(func() { clicks++ })
	button.Activate()

	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: B}})
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A, Repeat: true}})
	if clicks != 0 {
		t.Fatalf(`无效按键触发了点击：%d`, clicks)
	}
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	if clicks != 1 {
		t.Fatalf(`A 键没有触发一次点击：%d`, clicks)
	}
}

func TestDisabledButtonIgnoresClick(t *testing.T) {
	doc, button := newButtonDocument(t, `<document><block><button disabled><text>按钮</text></button></block></document>`)
	clicks := 0
	button.OnClick(func() { clicks++ })
	button.Activate()
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	if clicks != 0 || !button.Disabled() || !button.ClassContains(`disabled`) {
		t.Fatalf(`禁用按钮状态不正确：disabled=%v clicks=%d`, button.Disabled(), clicks)
	}

	button.SetDisabled(false)
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	if clicks != 1 || button.ClassContains(`disabled`) {
		t.Fatalf(`重新启用按钮失败：disabled=%v clicks=%d`, button.Disabled(), clicks)
	}
}

func newToggleDocument(t *testing.T, markup string) (*Document, *Toggle) {
	t.Helper()
	doc := _NewDocument(320, 240, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(markup)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	toggle := doc.QuerySelector[*Toggle](`toggle`)
	if toggle == nil {
		t.Fatal(`找不到 toggle`)
	}
	return doc, toggle
}

func TestToggleCheckedAttribute(t *testing.T) {
	_, toggle := newToggleDocument(t, `<document><block><toggle checked></toggle></block></document>`)
	if !toggle.Checked() || !toggle.ClassContains(`checked`) {
		t.Fatal(`checked 属性没有正确初始化 toggle`)
	}
}

func TestToggleRejectsChildren(t *testing.T) {
	doc := _NewDocument(320, 240, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><block><toggle><text>Wi-Fi</text></toggle></block></document>`)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err == nil {
		t.Fatal(`toggle 接受了子节点`)
	}
}

func TestActiveToggleChangesOnA(t *testing.T) {
	doc, toggle := newToggleDocument(t, `<document><block><toggle></toggle></block></document>`)
	changes := 0
	toggle.OnChange(func(checked bool) {
		changes++
		if !checked {
			t.Error(`状态变化事件携带了旧状态`)
		}
	})

	toggle.Activate()
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: B}})
	if toggle.Checked() || changes != 0 {
		t.Fatal(`非 A 键改变了 toggle 状态`)
	}

	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	if !toggle.Checked() || !toggle.ClassContains(`checked`) || changes != 1 {
		t.Fatalf(`A 键没有切换状态：checked=%v changes=%d`, toggle.Checked(), changes)
	}
}

func TestToggleIgnoresRepeatedADownEvents(t *testing.T) {
	doc, toggle := newToggleDocument(t, `<document><block><toggle></toggle></block></document>`)
	changes := 0
	toggle.OnChange(func(bool) { changes++ })
	toggle.Activate()

	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A, Repeat: true}})
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A, Repeat: true}})
	if !toggle.Checked() || changes != 1 {
		t.Fatalf(`重复的 A 键事件改变了状态：checked=%v changes=%d`, toggle.Checked(), changes)
	}
}

func TestToggleSetCheckedOnlyDispatchesForChanges(t *testing.T) {
	_, toggle := newToggleDocument(t, `<document><block><toggle></toggle></block></document>`)
	changes := 0
	toggle.OnChange(func(bool) { changes++ })

	toggle.SetChecked(true)
	toggle.SetChecked(true)
	toggle.SetChecked(false)
	if changes != 2 || toggle.ClassContains(`checked`) {
		t.Fatalf(`toggle 状态不正确：checked=%v changes=%d`, toggle.Checked(), changes)
	}
}

func TestToggleUsesIntrinsicSize(t *testing.T) {
	doc, toggle := newToggleDocument(t, `<document><block font-size="24"><toggle padding="3 7 5 11"></toggle></block></document>`)
	doc.layout()

	// 24 × 2.25 = 54；24 × 1.25 = 30。
	want := Rect{Width: 54 + 7 + 11, Height: 30 + 3 + 5}
	if got := toggle.GetLayoutBox(); got.Width != want.Width || got.Height != want.Height {
		t.Fatalf(`toggle 固有尺寸不正确：got=%+v want=%+v`, got, want)
	}
}

func TestToggleExplicitSizeOverridesFontSize(t *testing.T) {
	doc, toggle := newToggleDocument(t, `<document><block font-size="24"><toggle width="90" height="48"></toggle></block></document>`)
	doc.layout()
	if got, want := toggle.GetLayoutBox(), (Rect{Width: 90, Height: 48}); got.Width != want.Width || got.Height != want.Height {
		t.Fatalf(`显式尺寸没有覆盖固有尺寸：got=%+v want=%+v`, got, want)
	}
}

func TestToggleCustomColors(t *testing.T) {
	_, toggle := newToggleDocument(t, `<document><block><toggle track-color="#112233" checked-track-color="#445566" knob-color="#778899"></toggle></block></document>`)
	if want := ColorFromRGBA(0x11, 0x22, 0x33, 0xff); toggle.trackColor != want {
		t.Fatalf(`未选中轨道颜色不正确：got=%v want=%v`, toggle.trackColor, want)
	}
	if want := ColorFromRGBA(0x44, 0x55, 0x66, 0xff); toggle.checkedTrackColor != want {
		t.Fatalf(`选中轨道颜色不正确：got=%v want=%v`, toggle.checkedTrackColor, want)
	}
	if want := ColorFromRGBA(0x77, 0x88, 0x99, 0xff); toggle.knobColor != want {
		t.Fatalf(`滑块颜色不正确：got=%v want=%v`, toggle.knobColor, want)
	}
}

func TestToggleDrawsIndicatorAccordingToState(t *testing.T) {
	_, toggle := newToggleDocument(t, `<document><block><toggle></toggle></block></document>`)
	trackWidth, trackHeight := toggle.intrinsicSize()
	toggle.Calc(trackWidth, trackHeight, Constraints{})
	canvas := &Canvas{
		buffer: make([]byte, trackWidth*trackHeight*4),
		width:  trackWidth,
		height: trackHeight,
	}

	toggle.Draw(canvas)
	trackX := 0
	trackY := 0
	inset := min(trackWidth, trackHeight) / 10
	if got := canvas.getPixel(trackX, trackY); got != toggle.trackColor.NRGBA() {
		t.Fatalf(`未选中轨道颜色不正确：%v`, got)
	}
	if got := canvas.getPixel(trackX+inset, trackY+inset); got != toggle.knobColor.NRGBA() {
		t.Fatalf(`未选中滑块位置不正确：%v`, got)
	}

	toggle.SetChecked(true)
	toggle.Draw(canvas)
	if got := canvas.getPixel(trackX, trackY); got != toggle.checkedTrackColor.NRGBA() {
		t.Fatalf(`选中轨道颜色不正确：%v`, got)
	}
	knobSize := trackHeight - inset*2
	knobX := trackX + trackWidth - inset - knobSize
	if got := canvas.getPixel(knobX, trackY+inset); got != toggle.knobColor.NRGBA() {
		t.Fatalf(`选中滑块位置不正确：%v`, got)
	}
}
