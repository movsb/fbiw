package fbiw

import (
	"reflect"
	"testing"
	"testing/fstest"
	"time"
)

func TestThemeSchedule(t *testing.T) {
	location := time.FixedZone(`test`, 8*60*60)
	date := func(hour, minute int) time.Time {
		return time.Date(2026, time.September, 4, hour, minute, 0, 0, location)
	}
	for _, test := range []struct {
		now   time.Time
		light bool
	}{
		{date(5, 59), false},
		{date(6, 0), true},
		{date(17, 59), true},
		{date(18, 0), false},
	} {
		if got := isLightThemeTime(test.now); got != test.light {
			t.Errorf(`%s light=%v，期望 %v`, test.now, got, test.light)
		}
	}
}

func TestParseTheme(t *testing.T) {
	theme, err := (StyleParser{}).ParseTheme(`:root {
		--color-text: #112233;
		--color-primary: white;
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if theme.Colors[`--color-text`] != ColorFromString(`#112233`) ||
		theme.Colors[`--color-primary`] != ColorFromString(`white`) {
		t.Fatalf(`主题解析错误：%+v`, theme)
	}

	for name, source := range map[string]string{
		`缺少 root`: `document { --color-text: black; }`,
		`多个规则`:    `:root { --color-text: black; } block { color: red; }`,
		`非颜色变量`:   `:root { --spacing: 4; }`,
		`无效颜色`:    `:root { --color-text: wat; }`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := (StyleParser{}).ParseTheme(source); err == nil {
				t.Fatal(`无效主题未返回错误`)
			}
		})
	}
}

func testTheme(text, background, border, outline string) Theme {
	return Theme{
		Colors: map[string]Color{
			`--color-text`:       ColorFromString(text),
			`--color-background`: ColorFromString(background),
			`--color-border`:     ColorFromString(border),
			`--color-focus`:      ColorFromString(outline),
			`--color-required`:   ColorFromString(outline),
		},
	}
}

func TestThemeColorResolution(t *testing.T) {
	box := &BaseBox{Tag: `block`}
	sheet := Must1(ParseStyle(`block {
		color: var(--color-text);
		background-color: var(--color-background);
		border-color: var(--color-border);
		outline-color: var(--color-focus);
	}`))
	theme := testTheme(`#112233`, `#445566`, `#778899`, `#AABBCC`)
	if err := (_Styler{theme: theme, themeName: `light`}).Style(box, false, sheet); err != nil {
		t.Fatal(err)
	}
	styles := box.GetComputedStyles()
	if styles.Color != theme.Colors[`--color-text`] ||
		styles.BackgroundColor != theme.Colors[`--color-background`] ||
		styles.BorderColor != theme.Colors[`--color-border`] ||
		styles.OutlineColor != theme.Colors[`--color-focus`] {
		t.Fatalf(`主题颜色解析错误：%+v`, styles)
	}
}

func TestThemeColorErrors(t *testing.T) {
	for name, value := range map[string]string{
		`缺失颜色`:     `var(--color-missing)`,
		`非颜色令牌`:    `var(--spacing-small)`,
		`fallback`: `var(--color-text, red)`,
		`缺少右括号`:    `var(--color-text`,
	} {
		t.Run(name, func(t *testing.T) {
			box := &BaseBox{Tag: `block`}
			sheet := Must1(ParseStyle(`block { color: ` + value + `; }`))
			if err := (_Styler{themeName: `test`}).Style(box, false, sheet); err == nil {
				t.Fatalf(`颜色 %q 未返回错误`, value)
			}
		})
	}

}

func TestAppThemeSwitch(t *testing.T) {
	light := testTheme(`#111111`, `#EEEEEE`, `#333333`, `#555555`)
	dark := testTheme(`#EEEEEE`, `#111111`, `#CCCCCC`, `#AAAAAA`)
	app := newDesktopTestApp()
	app.themeManager.register(`light`, light)
	app.themeManager.register(`dark`, dark)
	activeDark := dark.clone()
	dark.Colors[`--color-text`] = ColorFromString(`#FF0000`)
	if err := app.SetThemeLight(`light`); err != nil {
		t.Fatal(err)
	}
	if err := app.SetThemeDark(`dark`); err != nil {
		t.Fatal(err)
	}
	noon := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{app: app}
	app.desktops.PushFront(desktop)
	doc := _NewDocument(100, 100, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><style>
			document { color: var(--color-text); }
			#target { outline-color: var(--color-required); }
		</style><block id="target" background-color="var(--color-background)"></block></document>`)},
	}, nil, nil)
	doc.bindApp(app)
	desktop.add(doc)
	defer doc.unbindApp()
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	target := doc.GetBoxByID[Box](`target`)
	assertColors := func(name string, theme Theme) {
		t.Helper()
		styles := target.GetComputedStyles()
		if styles.Color != theme.Colors[`--color-text`] || styles.BackgroundColor != theme.Colors[`--color-background`] {
			t.Fatalf(`主题 %q 未生效：%+v`, name, styles)
		}
	}
	assertColors(`light`, light)

	night := time.Date(2026, time.September, 4, 19, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(night); err != nil {
		t.Fatal(err)
	}
	assertColors(`dark`, activeDark)
	if app.ThemeName() != `dark` {
		t.Fatalf(`当前主题 = %q，期望 dark`, app.ThemeName())
	}

	before := app.Theme()
	app.themeManager.register(`broken`, Theme{})
	if err := app.SetThemeDark(`broken`); err == nil {
		t.Fatal(`缺失颜色的主题未返回错误`)
	}
	if got := app.Theme(); !reflect.DeepEqual(got, before) {
		t.Fatalf(`主题切换失败后 App 主题发生了变化：got=%+v want=%+v`, got, before)
	}
	assertColors(`dark`, activeDark)

	copyOfTheme := app.Theme()
	copyOfTheme.Colors[`--color-text`] = ColorFromString(`#FF0000`)
	assertColors(`dark`, activeDark)

	if err := target.SetProp(`background-color`, `#123456`); err != nil {
		t.Fatal(err)
	}
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}
	if got := target.GetComputedStyles().BackgroundColor; got != ColorFromString(`#123456`) {
		t.Fatalf(`固定内联颜色未覆盖主题变量：%v`, got)
	}
	if err := target.SetProp(`background-color`, `var(--color-missing)`); err == nil {
		t.Fatal(`未定义的内联主题变量未返回错误`)
	}
}

func TestBuiltInThemesAndPartialOverride(t *testing.T) {
	app := newDesktopTestApp()
	noon := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}
	light := app.Theme()
	if app.ThemeName() != defaultLightThemeName || light.Colors[`--color-control-background`] != ColorFromString(`#e8eaed`) {
		t.Fatalf(`系统浅色主题未生效：name=%q theme=%+v`, app.ThemeName(), light)
	}

	override := ColorFromString(`#123456`)
	app.themeManager.register(`custom`, Theme{Colors: map[string]Color{`--color-primary`: override}})
	if err := app.SetThemeLight(`custom`); err != nil {
		t.Fatal(err)
	}
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}
	custom := app.Theme()
	if custom.Colors[`--color-primary`] != override {
		t.Fatal(`自定义颜色未覆盖系统主题`)
	}
	if custom.Colors[`--color-selection-background`] != mixThemeColor(custom.Colors[`--color-surface`], override, 0.18) {
		t.Fatal(`自定义主题的 primary 未生成选中背景色`)
	}
	if custom.Colors[`--color-control-background`] != light.Colors[`--color-control-background`] {
		t.Fatal(`自定义主题未继承系统浅色主题`)
	}

	night := time.Date(2026, time.September, 4, 19, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(night); err != nil {
		t.Fatal(err)
	}
	if app.ThemeName() != defaultDarkThemeName || app.Theme().Colors[`--color-background`] != ColorFromString(`#11141b`) {
		t.Fatalf(`系统深色主题未生效：name=%q theme=%+v`, app.ThemeName(), app.Theme())
	}
}

func TestWithThemeReadsFile(t *testing.T) {
	app := newDesktopTestApp()
	themeFS := fstest.MapFS{
		`custom.css`: &fstest.MapFile{Data: []byte(`:root { --color-primary: #123456; }`)},
	}
	WithTheme(`custom`, themeFS, `custom.css`)(app)
	if err := app.SetThemeLight(`custom`); err != nil {
		t.Fatal(err)
	}
	noon := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}
	if got := app.Theme().Colors[`--color-primary`]; got != ColorFromString(`#123456`) {
		t.Fatalf(`文件主题未生效：%v`, got)
	}
}

func TestThemeAccent(t *testing.T) {
	app := newDesktopTestApp()
	before := app.Theme()

	darkAccent := ColorFromString(`#3358d4`)
	if err := app.SetThemeAccent(`#3358d4`); err != nil {
		t.Fatal(err)
	}
	theme := app.Theme()
	for _, token := range []string{`--color-primary`, `--color-primary-border`, `--color-focus`} {
		if theme.Colors[token] != darkAccent {
			t.Fatalf(`%s 未使用强调色`, token)
		}
	}
	for _, token := range []string{`--toggle-checked-track-color`, `--progress-value-color`} {
		if theme.Colors[token] != darkAccent {
			t.Fatalf(`%s 未跟随强调色：got=%v want=%v`, token, theme.Colors[token], darkAccent)
		}
	}
	selection := theme.Colors[`--color-selection-background`]
	ratio := Iif(isLightThemeTime(time.Now()), 0.18, 0.28)
	if selection != mixThemeColor(theme.Colors[`--color-surface`], darkAccent, ratio) {
		t.Fatalf(`选中背景色未从强调色生成：%v`, selection)
	}
	if theme.Colors[`--color-on-primary`] != ColorFromString(`#ffffff`) {
		t.Fatal(`深色强调色未自动使用白色前景`)
	}

	if err := app.SetThemeAccent(`#ffee00`); err != nil {
		t.Fatal(err)
	}
	if app.Theme().Colors[`--color-on-primary`] != ColorFromString(`#000000`) {
		t.Fatal(`浅色强调色未自动使用黑色前景`)
	}

	if err := app.ClearThemeAccent(); err != nil {
		t.Fatal(err)
	}
	if got := app.Theme(); !reflect.DeepEqual(got, before) {
		t.Fatalf(`清除强调色后未恢复主题：got=%+v want=%+v`, got, before)
	}
	if err := app.SetThemeAccent(`none`); err == nil {
		t.Fatal(`特殊颜色不应能作为强调色`)
	}
	if err := app.SetThemeAccent(`not-a-color`); err == nil {
		t.Fatal(`无效颜色字符串未返回错误`)
	}
}

func TestRegisteredThemeColor(t *testing.T) {
	variable := RegisterThemeColor(`--test-component-color`, `#112233`, `#ddeeff`)
	if got := defaultLightTheme().Colors[string(variable)]; got != ColorFromString(`#112233`) {
		t.Fatalf(`浅色默认值错误：%v`, got)
	}
	if got := defaultDarkTheme().Colors[string(variable)]; got != ColorFromString(`#ddeeff`) {
		t.Fatalf(`深色默认值错误：%v`, got)
	}

	theme := Must1((StyleParser{}).ParseTheme(`:root { --test-component-color: #abcdef; }`))
	app := newDesktopTestApp()
	app.themeManager.register(`component`, theme)
	if err := app.SetThemeLight(`component`); err != nil {
		t.Fatal(err)
	}
	noon := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}
	doc := _NewDocument(100, 100, fstest.MapFS{}, nil, nil)
	doc.bindApp(app)
	defer doc.unbindApp()
	if got := doc.ResolveThemeColor(variable); got != ColorFromString(`#abcdef`) {
		t.Fatalf(`主题覆盖值错误：%v`, got)
	}
}

func TestRegisteredThemeColorRejectsCycle(t *testing.T) {
	a := RegisterThemeColor(`--test-cycle-a`, `var(--test-cycle-b)`, `#000000`)
	b := RegisterThemeColor(`--test-cycle-b`, `var(--test-cycle-a)`, `#000000`)
	defer delete(registeredThemeColors, a)
	defer delete(registeredThemeColors, b)
	defer func() {
		if recover() == nil {
			t.Fatal(`循环引用未触发 panic`)
		}
	}()
	_ = defaultLightTheme()
}

func TestBuiltInWidgetThemeColors(t *testing.T) {
	theme := Must1((StyleParser{}).ParseTheme(`:root {
		--toggle-track-color: #102030;
		--toggle-checked-track-color: #405060;
		--toggle-knob-color: #708090;
		--progress-track-color: #a0b0c0;
		--progress-value-color: #d0e0f0;
	}`))
	app := newDesktopTestApp()
	app.themeManager.register(`widgets`, theme)
	if err := app.SetThemeLight(`widgets`); err != nil {
		t.Fatal(err)
	}
	noon := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.Local)
	if err := app.themeManager.applyForTime(noon); err != nil {
		t.Fatal(err)
	}

	doc := _NewDocument(100, 100, fstest.MapFS{
		`main.html`: &fstest.MapFile{Data: []byte(`<document><block><toggle></toggle><progress></progress></block></document>`)},
	}, nil, nil)
	doc.bindApp(app)
	defer doc.unbindApp()
	if err := doc.load(`main.html`); err != nil {
		t.Fatal(err)
	}
	toggle := doc.QuerySelector[*Toggle](`toggle`)
	progress := doc.QuerySelector[*ProgressBar](`progress`)
	if toggle.trackColor != ColorFromString(`#102030`) ||
		toggle.checkedTrackColor != ColorFromString(`#405060`) ||
		toggle.knobColor != ColorFromString(`#708090`) {
		t.Fatal(`toggle 未使用主题颜色`)
	}
	if progress.trackColor != ColorFromString(`#a0b0c0`) || progress.valueColor != ColorFromString(`#d0e0f0`) {
		t.Fatal(`progress 未使用主题颜色`)
	}
}
