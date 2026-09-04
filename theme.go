package fbiw

import (
	"fmt"
	"io/fs"
	"log"
	"maps"
	"time"
)

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

	timer *time.Timer
}

func newThemeManager(app *App) *ThemeManager {
	m := &ThemeManager{
		app:       app,
		themes:    map[string]Theme{},
		lightName: defaultLightThemeName,
		darkName:  defaultDarkThemeName,
	}
	m.themes[defaultLightThemeName] = defaultLightTheme()
	m.themes[defaultDarkThemeName] = defaultDarkTheme()
	_ = m.applyForTime(time.Now())
	return m
}

const (
	defaultLightThemeName = `__fbiw-light`
	defaultDarkThemeName  = `__fbiw-dark`
)

func defaultLightTheme() Theme {
	return _defaultLightTheme.clone()
}

func defaultDarkTheme() Theme {
	return _defaultDarkTheme.clone()
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
	if err := m.validateRegistered(name, defaultLightTheme()); err != nil {
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
	if err := m.validateRegistered(name, defaultDarkTheme()); err != nil {
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

func (m *ThemeManager) validateRegistered(name string, base Theme) error {
	theme, ok := m.resolved(name, base)
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

func (m *ThemeManager) resolved(name string, base Theme) (Theme, bool) {
	theme, ok := m.themes[name]
	if !ok {
		return Theme{}, false
	}
	resolved := base.clone()
	if resolved.Colors == nil {
		resolved.Colors = map[string]Color{}
	}
	maps.Copy(resolved.Colors, theme.Colors)
	return resolved, true
}

func (m *ThemeManager) apply(name string, base Theme) error {
	theme, ok := m.resolved(name, base)
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
	name := Iif(isLightThemeTime(now), m.lightName, m.darkName)
	if name == `` {
		name = Iif(m.lightName != ``, m.lightName, m.darkName)
	}

	if name == `` || name == m.name {
		return nil
	}

	base := Iif(isLightThemeTime(now) && m.lightName != ``, defaultLightTheme(), defaultDarkTheme())

	return m.apply(name, base)
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
