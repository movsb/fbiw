# fbiw

`fbiw` 是一个使用 Go 编写的轻量级 GUI 框架，主要面向使用 Linux framebuffer 和游戏手柄按键交互的固定屏幕设备。

它不依赖浏览器或 WebView，而是自行实现了一套精简的 HTML/CSS 风格界面描述、DOM、布局、事件传播和软件渲染系统。在 macOS 上，项目通过 SDL2 提供一个 `1024×768` 的开发窗口，便于在桌面环境中调试界面。

> 项目目前仍处于开发阶段，适合固定分辨率、方向键操作的掌机菜单和系统界面，不应视为完整的浏览器布局引擎或通用桌面 GUI 框架。

## 特性

- 使用类似 HTML 的文档描述界面；
- 支持标签、ID、class、后代和直接子元素等 CSS 选择器；
- 内置纵向、横向、叠层和虚拟列表布局；
- 支持颜色、背景图片、边框、内边距、尺寸、字体和对齐等样式；
- 支持 OpenType 字体、字形缓存和文本分段；
- 支持 PNG 等 Go `image` 包可解码的图片，并提供缩放缓存；
- 事件支持捕获、目标和冒泡阶段；
- 提供异步资源加载和主线程 UI 回调；
- Linux 使用 `/dev/fb0` 和 `/dev/input/event*`；
- macOS 使用 SDL2 模拟屏幕和按键。

## 环境要求

- Go `1.26.5`，以 [`go.mod`](go.mod) 的声明为准；
- macOS 开发环境需要安装 SDL2 及其开发文件；
- Linux 目标设备需要提供 framebuffer 和 evdev 输入设备，并允许程序访问：
  - `/dev/fb0`
  - `/dev/input/event*`

安装 Go 包：

```bash
go get github.com/movsb/fbiw
```

## 快速开始

准备一个界面文件 `main.html`：

```html
<document>
<style>
    #panel {
        width: 400;
        height: 240;
        padding: 20;
        background-color: white;
        align: both;
    }

    .title {
        color: black;
        font-size: 28;
    }
</style>

<block id="panel">
    <text class="title">Hello, fbiw!</text>
</block>
</document>
```

在 Go 中嵌入文档和字体并运行应用：

```go
package main

import (
    "context"
    "embed"

    "github.com/movsb/fbiw"
)

//go:embed main.html regular.ttf
var assets embed.FS

func main() {
    app := fbiw.NewApp(
        context.Background(),
        assets,
        fbiw.WithSystemFont(assets, "regular.ttf"),
    )
    defer app.Close()

    doc := app.New("main.html", ".")
    app.Show(doc)
    app.Run()
}
```

系统字体是必需资源。如果指定字体加载失败，真正绘制文字时会因为找不到可回退字体而终止。

## 文档结构

一个界面文档必须包含一个 `<document>` 根节点：

```html
<document>
    <style>
        /* 文档样式 */
    </style>

    <block>
        <!-- 唯一的内容根节点 -->
    </block>
</document>
```

规则如下：

- `<document>` 下最多有一个 `<style>`；
- 内容根节点只能有一个，且必须是 `<block>` 或 `<inline>`；
- 普通容器中不能直接放置非空文本，文字必须放在 `<text>` 中；
- `<b>` 和 `<i>` 只能出现在 `<text>`、`<b>` 或 `<i>` 内；
- `<img>` 和 `<spacer>` 是无子节点元素；
- 解析使用 Go 的 HTML5 parser，自定义标签不要使用 `<spacer/>` 形式，应写成 `<spacer></spacer>`。

## 内置组件

| 标签 | 用途 |
| --- | --- |
| `block` | 子元素纵向排列；未指定宽度的普通子元素默认使用可用宽度 |
| `inline` | 子元素单行横向排列 |
| `stack` | 子元素叠放在同一位置 |
| `scroll` | 固定行列、固定可视槽位的虚拟列表 |
| `spacer` | 在布局主轴上分配剩余空间 |
| `button` | 基于普通 Box 的语义化按钮容器 |
| `text` | 文本内容和文本分段 |
| `b` | 粗体文本片段 |
| `i` | 斜体文本片段 |
| `img` | 图片 |

也可以使用 `fbiw.Define` 注册实现了 `Box` 接口的自定义标签。

## 布局模型

`block` 沿垂直方向依次排列子元素，`inline` 沿水平方向依次排列子元素。`stack` 将所有子元素放置在同一个内容区域。

```html
<block height="300">
    <text>顶部</text>
    <spacer></spacer>
    <text>底部</text>
</block>
```

上面的 Spacer 会占据两个文本之间的剩余高度。在 `inline` 中使用 Spacer，则会占据剩余宽度：

```html
<inline width="400">
    <text>左侧</text>
    <spacer></spacer>
    <text>右侧</text>
</inline>
```

`align` 当前支持：

| 值 | 效果 |
| --- | --- |
| 空值 | 水平靠左、垂直靠上 |
| `center` | 水平居中 |
| `middle` | 垂直居中 |
| `both` | 水平和垂直居中 |

当前布局不是 Flexbox：`inline` 不会自动换行，也没有通用 margin、min/max size、绝对定位或通用 overflow 裁剪。

## 样式

样式可以写在 `<style>` 中，也可以直接作为元素属性：

```html
<block width="300" padding="12" background-color="#20242a">
    <text color="white" font-size="24">设置</text>
</block>
```

当前支持的样式属性：

- `align`
- `background-color`
- `background-image`
- `border-color`
- `border-width`
- `outline-color`
- `outline-width`
- `color`
- `width`
- `height`
- `padding`
- `font-family`
- `font-size`
- `bold` / `font-bold`
- `italic` / `font-italic`
- `spacer`
- `display`

`width` 和 `height` 可以解析整数或百分比。百分比布局目前仍有已知限制，参见 [`todo.md`](todo.md)。

颜色支持预置颜色名，以及 `#RGB`、`#RGBA`、`#RRGGBB`、`#RRGGBBAA` 十六进制形式。默认文本样式为：

```css
document {
    color: black;
    font-family: system;
    font-size: 25;
}
```

### 选择器

当前支持：

```css
block {}                 /* 标签 */
#main {}                 /* ID */
.selected {}             /* class */
block.item {}            /* 简单组合 */
scroll .selected {}      /* 后代 */
block > inline {}        /* 直接子元素 */
* {}                     /* 通配符 */
block, inline {}         /* 分组 */
```

样式覆盖顺序大致为：

```text
默认样式 < 文档样式表 < 元素内联属性
```

颜色和字体相关属性会从父元素继承。

## 查询和绑定元素

可以使用 ID 或选择器查询 Box：

```go
box := doc.GetBoxByID("panel")
first := doc.QuerySelector(".item")
all := doc.QuerySelectorAll("scroll .item")
```

`Bind` 可以根据结构体字段上的 `css` tag 自动绑定元素：

```go
type View struct {
    root  fbiw.Box
    title *fbiw.Text `css:"#title"`
    items []fbiw.Box `css:".item"`
}

var view View
doc.Bind(&view)
view.title.SetText("新的标题")
```

字段可以是单个 Box、具体组件指针或切片。名为 `root` 且类型为 `fbiw.Box` 的字段会绑定文档根元素。

`Unmarshal` 可用于动态创建一段组件树：

```go
type Item struct {
    root fbiw.Box
    text *fbiw.Text `css:"text"`
}

item := fbiw.Unmarshal[Item](doc, `
    <block background-color="tan">
        <text></text>
    </block>
`)
item.text.SetText("项目内容")
```

## 事件系统

Box 同时也是事件目标。事件按照捕获、目标和冒泡三个阶段传播：

```go
remove := box.Listen(fbiw.StickDownEvent, func(event *fbiw.Event) {
    if event.Stick.Name == fbiw.A {
        // 处理 A 键按下
        event.StopPropagation()
    }
}, fbiw.EventOptions{})

defer remove()
box.Activate()
```

捕获阶段监听器：

```go
doc.Listen(fbiw.StickDownEvent, func(event *fbiw.Event) {
    // 从根元素开始捕获事件
}, fbiw.EventOptions{Capture: true})
```

目前公开的输入事件主要是：

- `StickDownEvent`
- `StickUpEvent`
- `QuitEvent`

按键包括方向键、A/B/X/Y、Menu、Select、Start、Fn1/Fn2、音量、Home 和 L1/R1。同时按住 Menu 与 Start 会退出应用。

## 虚拟列表

`Scroll` 只创建 `rows × cols` 个可视组件，并在滚动时复用这些组件：

```html
<scroll
    id="scroll"
    rows="2"
    cols="3"
    gap="5"
    padding="10">
</scroll>
```

```go
scroll := doc.GetBoxByID("scroll").(*fbiw.Scroll)

type Item struct {
    root fbiw.Box
    text *fbiw.Text `css:"text"`
}

scroll.SetItems(
    100,
    func() (fbiw.Box, any) {
        item := fbiw.Unmarshal[Item](doc, `<block><text></text></block>`)
        return item.root, item
    },
    func(user any, index int) {
        item := user.(*Item)
        item.text.SetText(fmt.Sprintf("项目 %d", index))
    },
)

scroll.Activate()
```

被选中的可视槽位会自动获得 `.selected` class，可以通过样式显示选中状态：

```css
scroll .selected {
    outline-width: 3;
    outline-color: red;
}
```

`Scroll` 支持读取和恢复选择状态，但当前所有槽位尺寸相同，不支持可变高度列表。

## 图片和字体

相对图片路径基于 `app.New(name, skinDir)` 的 `skinDir`：

```html
<img src="icon.png" width="64" height="64">
<block background-image="panel.png"></block>
```

也可以通过 `os:` 来源读取操作系统文件。应用应只加载可信路径。

添加字体：

```go
app.AddFont("system", false, false, fontFS, "regular.ttf")
app.AddFont("system", true, false, fontFS, "bold.ttf")
```

或者在创建应用时使用：

```go
app := fbiw.NewApp(ctx, assets,
    fbiw.WithSystemFont(fontFS, "regular.ttf"),
    fbiw.WithFont("brand", false, false, fontFS, "brand.ttf"),
)
```

## 异步更新

UI 修改应在主事件线程执行。从其他 goroutine 更新界面时，可使用：

```go
app.Async(func() {
    text.SetText("加载完成")
})
```

仅需重绘时调用 `RequestPaint`，尺寸或结构变化时调用 `RequestLayout`。对应的 `RequestPaintAsync` 和 `RequestLayoutAsync` 可以直接从其他 goroutine 调用。

## 平台按键

macOS SDL2 开发窗口使用以下映射：

| 键盘 | fbiw 按键 |
| --- | --- |
| W / S / A / D | 上 / 下 / 左 / 右 |
| K / J | A / B |
| I / U | X / Y |
| R / T / Y | Menu / Select / Start |
| Q / O | L1 / R1 |

Linux 后端直接读取 evdev 按键码。当前设备选择和按键映射针对特定目标硬件编写，移植到其他设备时通常需要调整 [`linux.go`](linux.go)。

## 运行示例

仓库包含两个示例：

```bash
go run ./demo
go run ./demo/scroll
```

示例期望存在 `demo/regular.ttf`。该字体文件当前未包含在仓库中，运行前需要自行放置一个可用的 OpenType/TrueType 字体，并命名为 `regular.ttf`。

## 测试

运行全部测试：

```bash
go test ./...
```

测试目前覆盖样式解析、选择器查询、布局计算以及结构体绑定。平台后端、完整事件循环和异步图片加载尚缺少集成测试。

## 项目结构

```text
app.go        应用生命周期、文档堆叠和事件循环
common.go     按键与事件传播
dom.go        文档解析、DOM、查询、绑定和样式应用
style.go      CSS 子集、颜色和样式值
box.go        Box 组件与布局实现
canvas.go     BGRA 软件渲染和图片缓存
font.go       字体、字形缓存和文本测量
linux.go      Linux framebuffer/evdev 后端
macos.go      macOS SDL2 后端
demo/         示例程序
testdata/     布局、样式和查询测试数据
todo.md       已知问题与后续计划
```

## 已知限制

- 仅提供 Linux 和 macOS 平台后端；
- HTML 和 CSS 都是项目自定义的精简子集；
- Inline 当前是单行布局，不支持自动换行；
- 没有通用 Flex、Grid、margin、min/max size、绝对定位和裁剪；
- 百分比尺寸、溢出和负尺寸传播仍有待修复；
- Scroll 仅支持固定行列和等尺寸槽位；
- Linux 输入设备当前使用固定枚举位置，尚未按设备能力自动识别；
- framebuffer 后端采用整帧复制，不是真正的原子双缓冲；
- 公共 API 仍可能变化。

更完整的问题清单和优先级见 [`todo.md`](todo.md)。
