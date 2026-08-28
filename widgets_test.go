package fbiw

import (
	"strings"
	"testing"
	"testing/fstest"

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
	if got := dialog.view.popup.GetComputedStyles().Height.Number; got != 220 {
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
