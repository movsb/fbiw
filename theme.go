package fbiw

import (
	"fmt"
	"io/fs"
	"log"
	"maps"
	"math"
	"strings"
	"time"
)

// ThemeColor 是组件注册并从当前主题解析的颜色变量。
type ThemeColor string

type themeColorValue struct {
	color     Color
	reference ThemeColor
}

type themeColorDefaults struct {
	light themeColorValue
	dark  themeColorValue
}

var registeredThemeColors = map[ThemeColor]themeColorDefaults{}

// RegisterThemeColor 注册组件使用的主题颜色及其浅色、深色默认值。
// 组件包通常应在包初始化期间注册。
func RegisterThemeColor(name, lightDefault, darkDefault string) ThemeColor {
	if !strings.HasPrefix(name, `--`) || len(name) <= 2 || strings.ContainsAny(name, ",() \t\r\n") {
		panic(`无效主题颜色变量：` + name)
	}
	light := parseRegisteredThemeColorValue(lightDefault)
	dark := parseRegisteredThemeColorValue(darkDefault)
	variable := ThemeColor(name)
	defaults := themeColorDefaults{light: light, dark: dark}

	if previous, ok := registeredThemeColors[variable]; ok && previous != defaults {
		panic(`主题颜色变量重复注册且默认值不同：` + name)
	}
	registeredThemeColors[variable] = defaults
	return variable
}

func parseRegisteredThemeColorValue(raw string) themeColorValue {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, `var(`) && strings.HasSuffix(raw, `)`) {
		name := strings.TrimSpace(raw[4 : len(raw)-1])
		if !strings.HasPrefix(name, `--`) || len(name) <= 2 || strings.ContainsAny(name, ",() \t\r\n") {
			panic(`无效主题颜色引用：` + raw)
		}
		return themeColorValue{reference: ThemeColor(name)}
	}
	return themeColorValue{color: ColorFromString(raw)}
}

func withRegisteredThemeColors(theme Theme, light bool, overrides map[string]Color) Theme {
	resolved := theme.clone()
	if resolved.Colors == nil {
		resolved.Colors = map[string]Color{}
	}
	states := map[ThemeColor]uint8{}
	var resolve func(ThemeColor) Color
	resolve = func(name ThemeColor) Color {
		if states[name] == 1 {
			panic(`主题颜色变量循环引用：` + string(name))
		}
		if states[name] == 2 {
			return resolved.Colors[string(name)]
		}
		defaults, ok := registeredThemeColors[name]
		if !ok {
			color, exists := resolved.Colors[string(name)]
			if !exists {
				panic(`主题颜色引用不存在：` + string(name))
			}
			return color
		}
		if overrides != nil {
			if color, ok := overrides[string(name)]; ok {
				return color
			}
		}
		states[name] = 1
		value := Iif(light, defaults.light, defaults.dark)
		color := value.color
		if value.reference != `` {
			color = resolve(value.reference)
		}
		resolved.Colors[string(name)] = color
		states[name] = 2
		return color
	}
	for name := range registeredThemeColors {
		resolve(name)
	}
	return resolved
}

func isRegisteredThemeColor(name string) bool {
	_, ok := registeredThemeColors[ThemeColor(name)]
	return ok
}

// Theme 保存样式表可引用的语义颜色。
type Theme struct {
	Colors map[string]Color
}

func (t Theme) clone() Theme {
	out := Theme{}
	if t.Colors != nil {
		out.Colors = make(map[string]Color, len(t.Colors))
		maps.Copy(out.Colors, t.Colors)
	}
	return out
}

// ResolveColor 返回主题中指定的颜色。
func (t Theme) ResolveColor(name string) (Color, bool) {
	color, ok := t.Colors[name]
	return color, ok
}

// ThemeManager 管理 App 的主题注册、日夜配置和自动切换。
type ThemeManager struct {
	app *App

	// 所有已注册的主题。
	themes map[string]Theme

	// 当前主题及名字。
	theme Theme
	name  string

	lightName string
	darkName  string
	accent    *Color

	timer *time.Timer
}

func newThemeManager(app *App) *ThemeManager {
	m := &ThemeManager{
		app:       app,
		themes:    map[string]Theme{},
		lightName: defaultLightThemeName,
		darkName:  defaultDarkThemeName,
	}
	m.themes[defaultLightThemeName] = _defaultLightTheme.clone()
	m.themes[defaultDarkThemeName] = _defaultDarkTheme.clone()
	_ = m.applyForTime(time.Now())
	return m
}

const (
	defaultLightThemeName = `__fbiw-light`
	defaultDarkThemeName  = `__fbiw-dark`
)

func defaultLightTheme() Theme {
	return withRegisteredThemeColors(_defaultLightTheme, true, nil)
}

func defaultDarkTheme() Theme {
	return withRegisteredThemeColors(_defaultDarkTheme, false, nil)
}

func (m *ThemeManager) register(name string, theme Theme) {
	if name == `` {
		panic(`主题名不能为空`)
	}
	m.themes[name] = theme.clone()
}

// Theme 返回当前主题的快照。
func (m *ThemeManager) Theme() Theme {
	return m.theme.clone()
}

// Name 返回当前使用的主题名。
func (m *ThemeManager) Name() string {
	return m.name
}

// SetLight 设置早上 6 点到晚上 6 点使用的浅色主题。
func (m *ThemeManager) SetLight(name string) error {
	if err := m.validateRegistered(name, defaultLightTheme(), true); err != nil {
		return err
	}
	previous := m.lightName
	m.lightName = name
	if err := m.applyForTime(time.Now()); err != nil {
		m.lightName = previous
		return err
	}
	return nil
}

// SetDark 设置晚上 6 点到次早 6 点使用的深色主题。
func (m *ThemeManager) SetDark(name string) error {
	if err := m.validateRegistered(name, defaultDarkTheme(), false); err != nil {
		return err
	}
	previous := m.darkName
	m.darkName = name
	if err := m.applyForTime(time.Now()); err != nil {
		m.darkName = previous
		return err
	}
	return nil
}

func (m *ThemeManager) current() (string, Theme) {
	return m.name, m.theme
}

func (m *ThemeManager) validateRegistered(name string, base Theme, light bool) error {
	theme, ok := m.resolved(name, base, light)
	if !ok {
		return fmt.Errorf(`主题未注册：%s`, name)
	}
	for doc := range m.app.allDocuments() {
		if err := doc.validateTheme(name, theme); err != nil {
			return err
		}
	}
	return nil
}

func (m *ThemeManager) resolved(name string, base Theme, light bool) (Theme, bool) {
	theme, ok := m.themes[name]
	if !ok {
		return Theme{}, false
	}
	resolved := base.clone()
	if resolved.Colors == nil {
		resolved.Colors = map[string]Color{}
	}
	maps.Copy(resolved.Colors, theme.Colors)
	selectionColor := Color(0)
	deriveSelection := false
	if m.accent != nil {
		resolved.Colors[`--color-primary`] = *m.accent
		resolved.Colors[`--color-primary-border`] = *m.accent
		resolved.Colors[`--color-focus`] = *m.accent
		resolved.Colors[`--color-on-primary`] = accentForeground(*m.accent)
		selectionColor = *m.accent
		deriveSelection = true
	} else if primary, overridden := theme.Colors[`--color-primary`]; overridden {
		_, selectionOverridden := theme.Colors[`--color-selection-background`]
		selectionColor = primary
		deriveSelection = !selectionOverridden
	}
	if surface, ok := resolved.Colors[`--color-surface`]; ok && deriveSelection {
		ratio := Iif(light, 0.18, 0.28)
		resolved.Colors[`--color-selection-background`] = mixThemeColor(surface, selectionColor, ratio)
	}
	resolved = withRegisteredThemeColors(resolved, light, theme.Colors)
	return resolved, true
}

func mixThemeColor(base, overlay Color, ratio float64) Color {
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a)*(1-ratio) + float64(b)*ratio))
	}
	return ColorFromRGBA(
		mix(base.R(), overlay.R()),
		mix(base.G(), overlay.G()),
		mix(base.B(), overlay.B()),
		0xff,
	)
}

func accentForeground(accent Color) Color {
	linear := func(value uint8) float64 {
		channel := float64(value) / 255
		if channel <= 0.04045 {
			return channel / 12.92
		}
		return math.Pow((channel+0.055)/1.055, 2.4)
	}
	luminance := 0.2126*linear(accent.R()) + 0.7152*linear(accent.G()) + 0.0722*linear(accent.B())
	blackContrast := (luminance + 0.05) / 0.05
	whiteContrast := 1.05 / (luminance + 0.05)
	if blackContrast >= whiteContrast {
		return ColorFromRGBA(0, 0, 0, 0xff)
	}
	return ColorFromRGBA(0xff, 0xff, 0xff, 0xff)
}

func (m *ThemeManager) selectionForTime(now time.Time) (string, Theme, bool) {
	light := isLightThemeTime(now)
	name := Iif(light, m.lightName, m.darkName)
	if name == `` {
		name = Iif(m.lightName != ``, m.lightName, m.darkName)
		light = m.lightName != ``
	}
	return name, Iif(light, defaultLightTheme(), defaultDarkTheme()), light
}

func (m *ThemeManager) SetAccent(raw string) error {
	accent, err := ParseColor(raw)
	if err != nil {
		return fmt.Errorf(`无效强调色 %q：%w`, raw, err)
	}
	if accent == 0 || accent.IsNone() || accent.IsClear() || accent.A() != 0xff {
		return fmt.Errorf(`强调色必须是不透明颜色`)
	}
	previous := m.accent
	m.accent = &accent
	if err := m.apply(m.selectionForTime(time.Now())); err != nil {
		m.accent = previous
		return err
	}
	return nil
}

func (m *ThemeManager) ClearAccent() error {
	if m.accent == nil {
		return nil
	}
	previous := m.accent
	m.accent = nil
	name, base, light := m.selectionForTime(time.Now())
	if err := m.apply(name, base, light); err != nil {
		m.accent = previous
		return err
	}
	return nil
}

func (m *ThemeManager) apply(name string, base Theme, light bool) error {
	theme, ok := m.resolved(name, base, light)
	if !ok {
		return fmt.Errorf(`主题未注册：%s`, name)
	}
	for doc := range m.app.allDocuments() {
		if err := doc.validateTheme(name, theme); err != nil {
			return err
		}
	}

	previous := m.theme
	previousName := m.name
	m.theme = theme
	m.name = name
	updated := []*Document{}

	for doc := range m.app.allDocuments() {
		err := doc.restyle(name, theme)
		// 应用失败恢复所有前面已应用的到先前主题
		if err != nil {
			m.theme = previous
			m.name = previousName
			_ = doc.restyle(previousName, previous)
			for _, changed := range updated {
				_ = changed.restyle(previousName, previous)
			}
			return err
		}
		updated = append(updated, doc)
		doc.RequestPaint()
	}

	return nil
}

func (m *ThemeManager) applyForTime(now time.Time) error {
	name, base, light := m.selectionForTime(now)

	if name == `` || name == m.name {
		return nil
	}

	return m.apply(name, base, light)
}

func isLightThemeTime(now time.Time) bool {
	return now.Hour() >= 6 && now.Hour() < 18
}

const themeCheckInterval = time.Minute

func (m *ThemeManager) start() {
	m.scheduleCheck()
}

func (m *ThemeManager) close() {
	if m.timer != nil {
		m.timer.Stop()
	}
}

func (m *ThemeManager) scheduleCheck() {
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(themeCheckInterval, func() {
		select {
		case <-m.app.ctx.Done():
			return
		default:
		}
		m.app.Async(func() {
			select {
			case <-m.app.ctx.Done():
				return
			default:
			}
			if err := m.applyForTime(time.Now()); err != nil {
				log.Println(`自动切换主题失败:`, err)
			}
			m.scheduleCheck()
		})
	})
}

// WithTheme 从文件系统读取并注册一个可供 App 选用的主题。
func WithTheme(name string, fsys fs.FS, path string) Option {
	return func(app *App) {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			log.Printf(`读取主题 %q 时错误: %v`, name, err)
			return
		}
		theme, err := (StyleParser{}).ParseTheme(string(data))
		if err != nil {
			log.Printf(`解析主题 %q 时错误: %v`, name, err)
			return
		}
		app.themeManager.register(name, theme)
	}
}

func WithThemeLight(fsys fs.FS, path string) Option {
	return func(app *App) {
		WithTheme(`light`, fsys, path)(app)
		app.SetThemeLight(`light`)
	}
}

func WithThemeDark(fsys fs.FS, path string) Option {
	return func(app *App) {
		WithTheme(`dark`, fsys, path)(app)
		app.SetThemeDark(`dark`)
	}
}

// Theme 返回 App 当前主题的快照。
func (app *App) Theme() Theme {
	return app.themeManager.Theme()
}

// ThemeName 返回 App 当前使用的主题名。
func (app *App) ThemeName() string {
	return app.themeManager.Name()
}

// SetThemeLight 设置早上 6 点到晚上 6 点使用的浅色主题。
func (app *App) SetThemeLight(name string) error {
	return app.themeManager.SetLight(name)
}

// SetThemeDark 设置晚上 6 点到次早 6 点使用的深色主题。
func (app *App) SetThemeDark(name string) error {
	return app.themeManager.SetDark(name)
}

// SetThemeAccent 设置跨浅色、深色主题生效的强调色。
//
// 目前需要为不透明色，主要是为了让 --color-on-primary 的黑白自动选择可靠。
//
//   - 同一个半透明紫色放在白色和黑色背景上，最终亮度不同，因此无法只根据强调色本身判断应该配黑字还是白字。
//   - 强调色还会用于边框和焦点颜色，透明后视觉效果也更不稳定。
func (app *App) SetThemeAccent(color string) error {
	return app.themeManager.SetAccent(color)
}

// ClearThemeAccent 清除强调色覆盖，恢复当前主题原有颜色。
func (app *App) ClearThemeAccent() error {
	return app.themeManager.ClearAccent()
}
