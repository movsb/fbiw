package fbiw

import (
	"math"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/image/font/basicfont"
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
			if got, want := button.GetComputedStyles().BackgroundColor, ColorFromString(test.color); got != want {
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

func TestButtonCentersOversizedContent(t *testing.T) {
	doc, button := newButtonDocument(t, `<document><style>button { width: 360; height: 64; } button text { font-size: 50; }</style><block><button><text>按钮</text></button></block></document>`)
	doc.fontManager.faces[_FontFaceKey{Family: `system`, Size: 50}] = &FontFace{
		Face: basicfont.Face7x13, cache: map[rune]GlyphValue{},
	}
	doc.layout()

	text := button.children[0]
	contentWidth := button.GetLayoutBox().Width - button.HorizontalInsets()
	contentHeight := button.GetLayoutBox().Height - button.VerticalInsets()
	wantX := button.InsetLeft() + (contentWidth-text.Base().layoutBox.Width)/2
	wantY := button.InsetTop() + (contentHeight-text.Base().layoutBox.Height)/2
	if got := text.Base().layoutBox; got.X != wantX || got.Y != wantY {
		t.Fatalf(`button 内容没有居中：got=%+v wantX=%d wantY=%d`, got, wantX, wantY)
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

	// 这里仅验证静态绘制，直接设置逻辑状态和显示进度，不启动动画。
	toggle.checked = true
	toggle.knobProgress = 1
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

func newAnimatedToggle(t *testing.T, markup string, paint bool) (*App, *Document, *Toggle, *fakeAnimationTime) {
	t.Helper()
	app, placeholder, clock := newAnimationTestApp(t)
	doc, toggle := newToggleDocument(t, markup)
	desktop := placeholder.desktop
	desktop.remove(placeholder)
	doc.app = app
	desktop.add(doc)
	doc.layout()
	if paint {
		layout := toggle.GetLayoutBox()
		toggle.Draw(NewCanvas(layout.Width, layout.Height))
	}
	return app, doc, toggle, clock
}

func TestToggleAnimationInitialState(t *testing.T) {
	for _, checked := range []bool{false, true} {
		markup := `<document><block><toggle></toggle></block></document>`
		if checked {
			markup = `<document><block><toggle checked></toggle></block></document>`
		}
		app, doc, toggle, _ := newAnimatedToggle(t, markup, false)
		want := 0.0
		if checked {
			want = 1
		}
		if toggle.knobProgress != want {
			t.Fatal("初始状态未直接显示目标位置")
		}
		toggle.SetChecked(!checked)
		if toggle.knobProgress != 1-want || doc.timeline != nil || len(app.animation.requests) != 0 {
			t.Fatal("首次显示前不应播放动画")
		}
	}
}

func TestToggleAnimationPaintOnly(t *testing.T) {
	app, doc, toggle, clock := newAnimatedToggle(t, `<document><block><toggle></toggle></block></document>`, true)
	changes := 0
	toggle.OnChange(func(checked bool) {
		changes++
		if !checked || toggle.knobProgress != 0 {
			t.Fatal("逻辑事件未在动画之前生效")
		}
	})
	toggle.SetChecked(true)
	if !toggle.Checked() || !toggle.ClassContains("checked") || changes != 1 {
		t.Fatal("逻辑状态未立即更新")
	}
	// checked 类名可能改变布局；这里只验证后续动画帧不再请求布局。
	doc.layout()
	doc.layoutDirty, doc.paintDirty = false, false
	layout := toggle.GetLayoutBox()
	clock.now = clock.now.Add(toggleAnimationDuration / 2)
	animationStep(app)
	if toggle.knobProgress != 0.75 || doc.layoutDirty || !doc.paintDirty || toggle.GetLayoutBox() != layout {
		t.Fatal("动画未在中间位置只请求重绘")
	}
	canvas := NewCanvas(layout.Width, layout.Height)
	toggle.Draw(canvas)
	inset := min(layout.Width, layout.Height) / 10
	knobSize := layout.Height - inset*2
	knobX := inset + int(math.Round(float64(layout.Width-inset*2-knobSize)*0.75))
	if canvas.getPixel(knobX, inset) != toggle.knobColor.NRGBA() || canvas.getPixel(inset, inset) != toggle.checkedTrackColor.NRGBA() {
		t.Fatal("实际绘制的滑块未移动到补间位置")
	}
	clock.now = clock.now.Add(toggleAnimationDuration / 2)
	animationStep(app)
	if toggle.knobProgress != 1 || toggle.cancelTween != nil || app.animation.stop != nil || changes != 1 {
		t.Fatal("动画结束状态或事件次数不正确")
	}
}

func TestToggleAnimationReversesFromCurrentPosition(t *testing.T) {
	app, doc, toggle, clock := newAnimatedToggle(t, `<document><block><toggle></toggle></block></document>`, true)
	toggle.SetChecked(true)
	clock.now = clock.now.Add(toggleAnimationDuration / 2)
	animationStep(app)
	toggle.SetChecked(false)
	if toggle.knobProgress != 0.75 || len(doc.timeline.animations) != 1 {
		t.Fatal("反向切换跳变或旧动画未取消")
	}
	animation := doc.timeline.animations[0]
	toggle.SetChecked(false)
	if doc.timeline.animations[0] != animation {
		t.Fatal("相同状态重复设置重启了动画")
	}
	clock.now = clock.now.Add(toggleAnimationDuration / 2)
	animationStep(app)
	if toggle.knobProgress != 0.1875 {
		t.Fatal("没有从当前显示位置反向移动")
	}
	clock.now = clock.now.Add(toggleAnimationDuration / 2)
	animationStep(app)
	if toggle.knobProgress != 0 || len(doc.timeline.animations) != 0 {
		t.Fatal("旧动画覆盖了新目标")
	}
}

func TestToggleAnimationReentrantChange(t *testing.T) {
	app, _, toggle, _ := newAnimatedToggle(t, `<document><block><toggle></toggle></block></document>`, true)
	var states []bool
	toggle.OnChange(func(checked bool) {
		states = append(states, checked)
		if checked {
			toggle.SetChecked(false)
		}
	})
	toggle.SetChecked(true)
	if toggle.Checked() || toggle.knobProgress != 0 || toggle.cancelTween != nil || len(app.animation.requests) != 0 || !slices.Equal(states, []bool{true, false}) {
		t.Fatal("状态事件中反向切换后仍残留旧动画")
	}
}

func TestToggleAnimationAttributeAndLifecycle(t *testing.T) {
	app, doc, toggle, clock := newAnimatedToggle(t, `<document><block><toggle></toggle></block></document>`, true)
	toggle.OnChange(func(bool) { t.Fatal("SetProp 不应额外派发事件") })
	if err := toggle.SetProp("checked", "true"); err != nil {
		t.Fatal(err)
	}
	app.Detach()
	clock.now = clock.now.Add(time.Second)
	animationStep(app)
	if toggle.knobProgress != 0 || app.animation.stop != nil {
		t.Fatal("Detach 后没有暂停动画")
	}
	app.Attach()
	clock.now = clock.now.Add(animationFrameInterval)
	animationStep(app)
	if toggle.knobProgress != 1 {
		t.Fatal("恢复后没有追上进度")
	}
	if err := toggle.SetProp("checked", "false"); err != nil {
		t.Fatal(err)
	}
	doc.Close()
	clock.now = clock.now.Add(time.Second)
	animationStep(app)
	if toggle.knobProgress != 1 || len(app.animation.requests) != 0 {
		t.Fatal("文档关闭后仍更新滑块")
	}
	if err := toggle.SetProp("checked", "true"); err != nil {
		t.Fatal(err)
	}
	if toggle.cancelTween != nil {
		t.Fatal("关闭后的状态设置仍保留动画")
	}
}

func newProgressDocument(t *testing.T, markup string) (*Document, *ProgressBar) {
	t.Helper()
	doc := _NewDocument(640, 480, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(markup)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	progress := doc.QuerySelector[*ProgressBar](`progress`)
	if progress == nil {
		t.Fatal(`找不到 progress`)
	}
	return doc, progress
}

func TestProgressValue(t *testing.T) {
	doc, progress := newProgressDocument(t, `<document><block><progress></progress></block></document>`)
	if progress.Value() != 0 {
		t.Fatalf(`默认 value = %v，期望 0`, progress.Value())
	}
	if err := progress.SetValue(0.35); err != nil || progress.Value() != 0.35 {
		t.Fatalf(`设置 value 失败：value=%v err=%v`, progress.Value(), err)
	}

	doc.paintDirty = false
	if err := progress.SetValue(0.35); err != nil {
		t.Fatal(err)
	}
	if doc.paintDirty {
		t.Fatal(`相同 value 触发了重复重绘`)
	}

	_, fromHTML := newProgressDocument(t, `<document><block><progress value="0.75"></progress></block></document>`)
	if fromHTML.Value() != 0.75 {
		t.Fatalf(`HTML value = %v，期望 0.75`, fromHTML.Value())
	}
}

func TestProgressRejectsInvalidValues(t *testing.T) {
	_, progress := newProgressDocument(t, `<document><block><progress value="0.4"></progress></block></document>`)
	for _, value := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := progress.SetValue(value); err == nil {
			t.Fatalf(`非法 value %v 没有返回错误`, value)
		}
		if progress.Value() != 0.4 {
			t.Fatalf(`非法 value %v 改变了原值：%v`, value, progress.Value())
		}
	}
}

func TestProgressRejectsChildren(t *testing.T) {
	doc := _NewDocument(320, 240, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><block><progress><text>50%</text></progress></block></document>`)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err == nil {
		t.Fatal(`progress 接受了子节点`)
	}
}

func TestProgressCustomColors(t *testing.T) {
	_, progress := newProgressDocument(t, `<document><block><progress track-color="#112233" value-color="#445566"></progress></block></document>`)
	if want := ColorFromRGBA(0x11, 0x22, 0x33, 0xff); progress.trackColor != want {
		t.Fatalf(`轨道颜色不正确：got=%v want=%v`, progress.trackColor, want)
	}
	if want := ColorFromRGBA(0x44, 0x55, 0x66, 0xff); progress.valueColor != want {
		t.Fatalf(`完成颜色不正确：got=%v want=%v`, progress.valueColor, want)
	}
}

func TestProgressDrawsValue(t *testing.T) {
	_, progress := newProgressDocument(t, `<document><block><progress width="10" height="4"></progress></block></document>`)
	progress.Calc(10, 4, Constraints{})
	canvas := &Canvas{buffer: make([]byte, 10*4*4), width: 10, height: 4}

	progress.Draw(canvas)
	if got := canvas.getPixel(0, 0); got != progress.trackColor.NRGBA() {
		t.Fatalf(`0%% 轨道颜色不正确：%v`, got)
	}

	if err := progress.SetValue(0.25); err != nil {
		t.Fatal(err)
	}
	progress.Draw(canvas)
	if got := canvas.getPixel(2, 0); got != progress.valueColor.NRGBA() {
		t.Fatalf(`25%% 完成区域宽度不足：%v`, got)
	}
	if got := canvas.getPixel(3, 0); got != progress.trackColor.NRGBA() {
		t.Fatalf(`25%% 完成区域宽度过大：%v`, got)
	}

	if err := progress.SetValue(1); err != nil {
		t.Fatal(err)
	}
	progress.Draw(canvas)
	if got := canvas.getPixel(9, 3); got != progress.valueColor.NRGBA() {
		t.Fatalf(`100%% 没有铺满内容区：%v`, got)
	}
}

func TestProgressIntrinsicAndExplicitSize(t *testing.T) {
	doc, progress := newProgressDocument(t, `<document><block font-size="24"><progress padding="3 7 5 11"></progress></block></document>`)
	doc.layout()
	want := Rect{Width: 24*8 + progress.HorizontalInsets(), Height: 24/2 + progress.VerticalInsets()}
	if got := progress.GetLayoutBox(); got.Width != want.Width || got.Height != want.Height {
		t.Fatalf(`progress 固有尺寸不正确：got=%+v want=%+v`, got, want)
	}

	doc, progress = newProgressDocument(t, `<document><style>progress { width: 410; height: 18; background-color: #123456; border-width: 2; }</style><block><progress></progress></block></document>`)
	doc.layout()
	if got := progress.GetLayoutBox(); got.Width != 410 || got.Height != 18 {
		t.Fatalf(`CSS 尺寸覆盖失败：%+v`, got)
	}
	if got, want := progress.GetComputedStyles().BackgroundColor, ColorFromString(`#123456`); got != want {
		t.Fatalf(`CSS 背景覆盖失败：got=%v want=%v`, got, want)
	}
}

func TestProgressDoesNotHandleInput(t *testing.T) {
	doc, progress := newProgressDocument(t, `<document><block><progress value="0.5"></progress></block></document>`)
	progress.Activate()
	for _, name := range []KeyName{A, B, Left, Right, Up, Down} {
		doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: name, Repeat: true}})
	}
	if progress.Value() != 0.5 {
		t.Fatalf(`输入事件改变了 progress：%v`, progress.Value())
	}
}

func newSelectDocument(t *testing.T, markup string) (*Document, *SelectBox) {
	t.Helper()
	doc := _NewDocument(640, 480, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(markup)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	selectBox := doc.QuerySelector[*SelectBox](`select`)
	if selectBox == nil {
		t.Fatal(`找不到 select`)
	}
	return doc, selectBox
}

func newSelectPopupDocument(t *testing.T, markup string) (*App, *Document, *SelectBox) {
	t.Helper()
	app := newDesktopTestApp()
	app.canvas = &Canvas{width: 1024, height: 768}
	app.images = NewImageManager()
	app.fonts = NewFontManager()
	app.fonts.faces[_FontFaceKey{Family: `system`, Size: 32}] = &FontFace{
		Face: basicfont.Face7x13, cache: map[rune]GlyphValue{},
	}
	doc := app.NewDesktop(fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(markup)},
	}, `main.html`)
	selectBox := doc.QuerySelector[*SelectBox](`select`)
	if selectBox == nil {
		t.Fatal(`找不到 select`)
	}
	return app, doc, selectBox
}

func sendSelectPopupKey(b *SelectBox, name KeyName, repeat bool) {
	b.popup.handleEvent(&Event{
		Type:  StickDownEvent,
		Stick: KeyEventArgs{Name: name, Repeat: repeat},
	})
}

func TestSelectItemsIndexAndChange(t *testing.T) {
	_, b := newSelectDocument(t, `<document><block><select placeholder="选择语言"></select></block></document>`)
	if b.Index() != -1 || b.placeholder != `选择语言` {
		t.Fatal(`select 初始状态不正确`)
	}

	items := []string{`中文`, `English`, `日本語`}
	b.SetItems(items)
	items[0] = `已修改`
	copyOfItems := b.Items()
	copyOfItems[1] = `已修改`
	if got := b.Items(); got[0] != `中文` || got[1] != `English` {
		t.Fatalf(`SetItems 或 Items 没有复制切片：%v`, got)
	}

	var changes []int
	b.OnChange(func(index int) { changes = append(changes, index) })
	if err := b.SetIndex(1); err != nil {
		t.Fatal(err)
	}
	b.SetIndex(1)
	selected, ok := b.Selected()
	if !ok || selected != `English` || b.Index() != 1 {
		t.Fatal(`设置索引后没有返回已选项`)
	}
	if err := b.SetIndex(3); err == nil {
		t.Fatal(`越界索引没有返回错误`)
	}
	if err := b.SetIndex(-1); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Selected(); ok {
		t.Fatal(`清空后仍返回已选项`)
	}
	if got, want := changes, []int{1, -1}; !slices.Equal(got, want) {
		t.Fatalf(`变化事件 = %v，期望 %v`, got, want)
	}

	b.SetIndex(2)
	b.SetItems([]string{`仅一项`})
	if b.Index() != -1 || changes[len(changes)-1] != -1 {
		t.Fatal(`SetItems 没有清除越界的当前索引`)
	}
}

func TestSelectRejectsChildren(t *testing.T) {
	doc := _NewDocument(320, 240, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><block><select><text>错误</text></select></block></document>`)},
	}, NewFontManager(), NewImageManager())
	if err := doc.load(`main.html`); err == nil {
		t.Fatal(`select 接受了子节点`)
	}
}

func TestSelectPopupCommitCancelAndRepeat(t *testing.T) {
	_, doc, b := newSelectPopupDocument(t, `<document><block><select></select></block></document>`)
	b.SetItems([]string{`一`, `二`, `三`})
	b.Activate()
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	if !b.Opened() || b.popupView.list.DataIndex() != -1 {
		t.Fatal(`打开时不应自动高亮首项`)
	}

	sendSelectPopupKey(b, Down, false)
	sendSelectPopupKey(b, Down, true)
	if got := b.popupView.list.DataIndex(); got != 1 {
		t.Fatalf(`方向键重复导航后的索引 = %d，期望 1`, got)
	}
	sendSelectPopupKey(b, A, true)
	if !b.Opened() || b.Index() != -1 {
		t.Fatal(`重复 A 提交了选项`)
	}
	closedDuringCallback := false
	b.OnChange(func(int) { closedDuringCallback = !b.Opened() })
	sendSelectPopupKey(b, A, false)
	if b.Opened() || b.Index() != 1 || !closedDuringCallback {
		t.Fatal(`A 没有先关闭 Popup 再提交`)
	}

	b.Open()
	if got := b.popupView.list.DataIndex(); got != 1 {
		t.Fatalf(`重新打开没有恢复当前高亮：%d`, got)
	}
	sendSelectPopupKey(b, Up, false)
	sendSelectPopupKey(b, B, true)
	if !b.Opened() {
		t.Fatal(`重复 B 关闭了 Popup`)
	}
	sendSelectPopupKey(b, B, false)
	if b.Opened() || b.Index() != 1 {
		t.Fatal(`B 取消时改变了当前值`)
	}
}

func TestSelectEmptyDisabledAndIdempotent(t *testing.T) {
	_, doc, b := newSelectPopupDocument(t, `<document><block><select disabled></select></block></document>`)
	b.Activate()
	doc.handleEvent(&Event{Type: StickDownEvent, Stick: KeyEventArgs{Name: A}})
	if b.Opened() || !b.Disabled() {
		t.Fatal(`禁用的 select 被打开`)
	}

	b.SetDisabled(false)
	b.Open()
	b.Open()
	if !b.Opened() || displaying(b.popupView.list) || !displaying(b.popupView.empty) {
		t.Fatal(`空列表 Popup 状态不正确`)
	}
	sendSelectPopupKey(b, A, false)
	if !b.Opened() {
		t.Fatal(`空列表响应了 A`)
	}
	b.SetDisabled(true)
	if b.Opened() {
		t.Fatal(`动态禁用没有关闭 Popup`)
	}
	b.Close()
	b.Close()
}

func TestSelectRestoresLongListPosition(t *testing.T) {
	_, _, b := newSelectPopupDocument(t, `<document><block><select></select></block></document>`)
	b.SetItems([]string{`0`, `1`, `2`, `3`, `4`, `5`, `6`, `7`, `8`})
	if err := b.SetIndex(8); err != nil {
		t.Fatal(err)
	}
	b.Open()
	if got := b.popupView.list.DataIndex(); got != 8 {
		t.Fatalf(`长列表没有恢复当前高亮：got=%d want=8`, got)
	}
	if got := b.popupView.list.RowIndex(); got < 0 || got >= 7 {
		t.Fatalf(`恢复后的高亮不在可视范围：row=%d`, got)
	}
}

func TestSelectIntrinsicAndExplicitSize(t *testing.T) {
	doc, b := newSelectDocument(t, `<document><block font-size="24"><select padding="3 7 5 11"></select></block></document>`)
	doc.layout()
	if got, want := b.GetLayoutBox(), (Rect{Width: 24*9 + b.HorizontalInsets(), Height: 24*3/2 + b.VerticalInsets()}); got.Width != want.Width || got.Height != want.Height {
		t.Fatalf(`select 固有尺寸不正确：got=%+v want=%+v`, got, want)
	}

	doc, b = newSelectDocument(t, `<document><style>select { width: 410; height: 70; background-color: #123456; }</style><block><select></select></block></document>`)
	doc.layout()
	if got := b.GetLayoutBox(); got.Width != 410 || got.Height != 70 {
		t.Fatalf(`CSS 尺寸覆盖失败：%+v`, got)
	}
	if got, want := b.GetComputedStyles().BackgroundColor, ColorFromString(`#123456`); got != want {
		t.Fatalf(`CSS 背景覆盖失败：got=%v want=%v`, got, want)
	}
}

func newAlertDialogTestApp() (*App, *Document) {
	app := newDesktopTestApp()
	app.canvas = &Canvas{width: 1024, height: 768}
	app.images = NewImageManager()
	app.fonts = NewFontManager()
	for _, size := range []int{28, 32, 43} {
		app.fonts.faces[_FontFaceKey{Family: `system`, Size: size}] = &FontFace{
			Face:  basicfont.Face7x13,
			cache: map[rune]GlyphValue{},
		}
	}

	desktop := &Desktop{app: app}
	app.desktops.PushFront(desktop)
	opener := &Document{app: app}
	desktop.add(opener)
	return app, opener
}

func sendAlertDialogKey(dialog *AlertDialog, name KeyName, repeat bool) {
	dialog.document.handleEvent(&Event{
		Type:  StickDownEvent,
		Stick: KeyEventArgs{Name: name, Repeat: repeat},
	})
}

func TestSingleButtonAlertDialog(t *testing.T) {
	app, opener := newAlertDialogTestApp()
	actions := 0
	dialog := app.ShowAlertDialog(opener, AlertDialogOptions{
		Title:    `提示`,
		OnAction: func() { actions++ },
	})

	if got := dialog.view.actionText.GetText(); got != `确定` {
		t.Fatalf(`默认操作文字 = %q`, got)
	}
	if dialog.view.action.Variant() != ButtonPrimary {
		t.Fatalf(`默认操作样式 = %q`, dialog.view.action.Variant())
	}
	if displaying(dialog.view.cancel) {
		t.Fatal(`单按钮弹窗显示了取消按钮`)
	}

	sendAlertDialogKey(dialog, B, false)
	if dialog.Closed() || actions != 0 {
		t.Fatal(`单按钮弹窗响应了 B 键`)
	}
	sendAlertDialogKey(dialog, A, true)
	if dialog.Closed() || actions != 0 {
		t.Fatal(`重复 A 键触发了操作`)
	}
	sendAlertDialogKey(dialog, A, false)
	if !dialog.Closed() || actions != 1 {
		t.Fatalf(`A 键操作失败：closed=%v actions=%d`, dialog.Closed(), actions)
	}
}

func TestTwoButtonAlertDialogActions(t *testing.T) {
	tests := []struct {
		name       string
		key        KeyName
		wantAction int
		wantCancel int
	}{
		{`确认`, A, 1, 0},
		{`取消`, B, 0, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, opener := newAlertDialogTestApp()
			actions, cancels := 0, 0
			var dialog *AlertDialog
			dialog = app.ShowAlertDialog(opener, AlertDialogOptions{
				Title:         `删除存档？`,
				Description:   `此操作无法撤销。`,
				ActionText:    `删除`,
				ActionVariant: ButtonDestructive,
				CancelText:    `取消`,
				OnAction: func() {
					if !dialog.Closed() {
						t.Error(`Action 回调发生在关闭之前`)
					}
					actions++
				},
				OnCancel: func() {
					if !dialog.Closed() {
						t.Error(`Cancel 回调发生在关闭之前`)
					}
					cancels++
				},
			})
			if dialog.view.action.Variant() != ButtonDestructive || dialog.view.cancelText.GetText() != `取消` {
				t.Fatal(`双按钮配置没有应用`)
			}
			sendAlertDialogKey(dialog, test.key, true)
			sendAlertDialogKey(dialog, X, false)
			if actions != 0 || cancels != 0 || dialog.Closed() {
				t.Fatal(`重复或无关按键触发了动作`)
			}
			sendAlertDialogKey(dialog, test.key, false)
			if actions != test.wantAction || cancels != test.wantCancel || !dialog.Closed() {
				t.Fatalf(`结果不正确：actions=%d cancels=%d closed=%v`, actions, cancels, dialog.Closed())
			}
		})
	}
}

func TestAlertDialogDescriptionScrolls(t *testing.T) {
	app, opener := newAlertDialogTestApp()
	dialog := app.ShowAlertDialog(opener, AlertDialogOptions{
		Title:       `使用条款`,
		Description: strings.Repeat(`这是一段很长的说明文字。`, 100),
	})
	dialog.document.layout()
	if len(dialog.view.description.textLines) <= 1 {
		t.Fatal(`长说明文字没有折行`)
	}
	if got := dialog.view.descriptionViewport.GetLayoutBox().Height; got != 260 {
		t.Fatalf(`说明视口高度 = %d，期望 260`, got)
	}

	sendAlertDialogKey(dialog, Down, true)
	if dialog.view.description.textDrawLineOffset != 1 {
		t.Fatalf(`重复 Down 没有滚动：offset=%d`, dialog.view.description.textDrawLineOffset)
	}
	sendAlertDialogKey(dialog, Up, true)
	if dialog.view.description.textDrawLineOffset != 0 {
		t.Fatalf(`重复 Up 没有滚动：offset=%d`, dialog.view.description.textDrawLineOffset)
	}
}

func TestAlertDialogEmptyDescriptionHidesViewport(t *testing.T) {
	app, opener := newAlertDialogTestApp()
	dialog := app.ShowAlertDialog(opener, AlertDialogOptions{Title: `提示`})
	if displaying(dialog.view.descriptionViewport) || displaying(dialog.view.descriptionGap) {
		t.Fatal(`空 Description 没有隐藏说明区域`)
	}
	if got := dialog.view.popup.GetComputedStyles().Height.Number(); got != 220 {
		t.Fatalf(`空 Description 的弹窗高度 = %d，期望 220`, got)
	}
}

func TestAlertDialogCloseIsIdempotent(t *testing.T) {
	app, opener := newAlertDialogTestApp()
	openerActive := NewBlock(opener)
	opener.activeBox = openerActive
	actions := 0
	dialog := app.ShowAlertDialog(opener, AlertDialogOptions{
		Title:    `提示`,
		OnAction: func() { actions++ },
	})
	dialog.Close()
	dialog.Close()
	if !dialog.Closed() || actions != 0 || opener.ActiveBox() != openerActive {
		t.Fatalf(`程序关闭行为不正确：closed=%v actions=%d active=%p`, dialog.Closed(), actions, opener.ActiveBox())
	}
}

func TestAlertDialogInvalidOptionsPanic(t *testing.T) {
	tests := []AlertDialogOptions{
		{},
		{Title: `提示`, OnCancel: func() {}},
		{Title: `提示`, ActionVariant: ButtonVariant(`unknown`)},
	}
	for index, options := range tests {
		t.Run(string(rune('0'+index)), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal(`无效配置没有 panic`)
				}
			}()
			app, opener := newAlertDialogTestApp()
			app.ShowAlertDialog(opener, options)
		})
	}
}

func TestAlertDialogInvalidOpenerPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal(`无效 opener 没有 panic`)
		}
	}()
	app, _ := newAlertDialogTestApp()
	app.ShowAlertDialog(&Document{}, AlertDialogOptions{Title: `提示`})
}
