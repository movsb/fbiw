# fbiw

`fbiw` 是一个使用 Go 编写的轻量级 GUI 框架，主要面向使用 Linux framebuffer 和游戏手柄按键交互的固定屏幕设备。

它不依赖浏览器或 WebView，而是自行实现了一套精简的 HTML/CSS 风格界面描述、DOM、布局、事件传播和软件渲染系统。在 macOS 上，项目通过 SDL2 提供一个 `1024×768` 的开发窗口，便于在桌面环境中调试界面。

> 项目目前仍处于开发阶段，适合固定分辨率、方向键操作的掌机菜单和系统界面，不应视为完整的浏览器布局引擎或通用桌面 GUI 框架。

## 特性

- 使用类似 HTML 的文档描述界面；
- 支持标签、ID、class、后代和直接子元素等 CSS 选择器；
- 内置纵向、横向、叠层和虚拟列表布局；
- 支持始终位于文档栈上方的系统覆盖层，以及供文档避让覆盖层的安全区域；
- 支持颜色、背景图片、边框、内边距、尺寸、字体和对齐等样式；
- 支持 OpenType 字体、字形缓存和文本分段；
- 支持 PNG 等 Go `image` 包可解码的图片，并提供缩放缓存；
- 事件支持捕获、目标和冒泡阶段；
- 提供异步资源加载和主线程 UI 回调；
- Linux 使用 `/dev/fb0` 和 `/dev/input/event*`；
- macOS 使用 SDL2 模拟屏幕和按键。

## 环境要求

- Go `1.27`，以 [`go.mod`](go.mod) 的声明为准；当前使用实验性的 `simd/archsimd`，构建和测试时需要设置 `GOEXPERIMENT=simd`；
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
    "embed"

    "github.com/movsb/fbiw"
)

//go:embed main.html regular.ttf
var assets embed.FS

func main() {
    app := fbiw.NewApp(
        fbiw.WithSystemFont(assets, "regular.ttf"),
    )
    defer app.Close()

    doc := app.New(assets, "main.html")
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
- 内容根节点只能有一个，且必须是 `<block>`、`<inline>` 或 `<stack>`；
- 普通容器中不能直接放置非空文本，文字必须放在 `<text>` 中；
- `<b>` 和 `<i>` 只能出现在 `<text>`、`<b>` 或 `<i>` 内；
- `<img>` 和 `<spacer>` 是无子节点元素；
- 解析使用 Go 的 HTML5 parser，自定义标签不要使用 `<spacer/>` 形式，应写成 `<spacer></spacer>`。

## 内置组件

| 标签 | 用途 |
| --- | --- |
| `block` | 子元素纵向排列 |
| `inline` | 子元素单行横向排列 |
| `stack` | 子元素叠放在同一位置 |
| `safe-area` | 根据系统覆盖层占用的四边区域，为内容设置安全内边距 |
| `scroll` | 固定行列、固定可视槽位的虚拟列表 |
| `spacer` | 在布局主轴上分配剩余空间 |
| `button` | 带默认样式、A 键交互和禁用状态的按钮容器 |
| `toggle` | 不接受子节点，激活后按 A 键切换 checked 状态的开关 |
| `progress` | 不接受子节点，绘制 `[0,1]` 范围内的确定进度 |
| `select` | 不接受子节点，使用模态列表选择预定义选项 |
| `text` | 文本内容和文本分段 |
| `b` | 粗体文本片段 |
| `i` | 斜体文本片段 |
| `img` | 图片 |

也可以使用 `fbiw.Define` 注册实现了 `Box` 接口的自定义标签。

`button` 支持普通、主按钮和危险操作三种样式，以及禁用状态：

```html
<button><text>普通按钮</text></button>
<button variant="primary"><text>主按钮</text></button>
<button variant="destructive"><text>删除</text></button>
<button disabled><text>不可用</text></button>
```

激活 Button 后按 A 键会触发点击；按住 A 产生的重复事件会被忽略：

```go
button := doc.GetBoxByID[*fbiw.Button]("submit")
remove := button.OnClick(func() {
    submit()
})
defer remove()

button.Activate()
button.SetDisabled(false)
```

也可以使用 `SetVariant` 动态切换 `fbiw.ButtonNormal`、
`fbiw.ButtonPrimary` 和 `fbiw.ButtonDestructive`。默认样式由框架样式表提供，
文档中的 CSS 可以继续覆盖背景、文字、边框、尺寸和间距。

Alert Dialog 使用 Popup 文档显示在当前文档之上，由框架生成背景遮罩、
标题、可滚动说明和一到两个按钮：

```go
app.ShowAlertDialog(doc, fbiw.AlertDialogOptions{
    Title:         "删除存档？",
    Description:   "此操作无法撤销。",
    ActionText:    "删除",
    ActionVariant: fbiw.ButtonDestructive,
    CancelText:    "取消",
    OnAction: func() {
        deleteSave()
    },
    OnCancel: func() {
        log.Println("已取消")
    },
})
```

操作规则如下：

- A 执行确认操作；
- 有取消按钮时，B 执行取消；单按钮弹窗忽略 B；
- 上下键逐行滚动较长的 Description；
- 动作触发时先关闭弹窗，再调用相应回调；
- `ShowAlertDialog` 返回的对象可以通过 `Close()` 无回调地关闭。

`ActionText` 默认为“确定”，`ActionVariant` 默认为
`fbiw.ButtonPrimary`。`CancelText` 为空时只显示一个按钮。

`toggle` 只绘制开关本身，文字等内容由外部元素提供：

```html
<inline align="middle">
    <text>Wi-Fi</text>
    <spacer></spacer>
    <toggle id="wifi"></toggle>
</inline>
```

默认尺寸跟随其计算后的 `font-size`：宽度为 `2.25em`，高度为
`1.25em`。也可以使用 `width` 和 `height` 显式覆盖。轨道和滑块颜色
可以通过元素属性设置：

```html
<toggle
    track-color="#656b76"
    checked-track-color="#34c759"
    knob-color="white">
</toggle>
```

在 Go 中激活 Toggle，并使用类型安全的 `OnChange` 监听状态变化：

```go
toggle := doc.GetBoxByID[*fbiw.Toggle]("wifi")
remove := toggle.OnChange(func(checked bool) {
    log.Println("Wi-Fi:", checked)
})
defer remove()

toggle.Activate()
```

`OnChange` 返回解除监听的函数。需要访问事件目标、传播阶段或调用
`StopPropagation` 时，可以改用底层的 `Listen` 和
`fbiw.ToggleChangeEvent`。

`progress` 只绘制轨道和完成部分，进度使用 `[0,1]` 范围内的浮点数：

```html
<progress
    id="download"
    value="0.35"
    track-color="#656b76"
    value-color="#3358d4">
</progress>
```

```go
progress := doc.GetBoxByID[*fbiw.ProgressBar]("download")
err := progress.SetValue(float64(completed) / float64(total))
```

标题、两端数值和百分比文字由外部元素提供。两端文字可以和 Progress
一起放在 `block` 中，中间覆盖的百分比可以使用 `stack` 将文字叠在
Progress 上方。默认尺寸为 `8em × 0.5em`，也可以通过 CSS 覆盖尺寸、
padding、背景和边框。

首版仅支持确定进度，不支持未知进度的循环动画；该模式将在统一动画系统
可用后实现。

`select` 用于从预定义项目中选择一项。它会显示当前值或 placeholder，
激活后按 A 打开居中的列表；方向键移动高亮，A 提交，B 取消：

```html
<select id="language" placeholder="请选择语言"></select>
<select id="device" disabled></select>
```

```go
language := doc.GetBoxByID[*fbiw.SelectBox]("language")
language.SetItems([]string{"简体中文", "English", "日本語"})
language.OnChange(func(index int) {
    value, _ := language.Selected()
    log.Println(index, value)
})
language.Activate()
```

`SelectBox` 支持 `SetIndex(-1)` 清空选择，`Items()` 返回项目副本；
动态调用 `SetDisabled(true)` 会关闭已打开的列表。方向键的重复事件用于
按住连续导航，而重复的 A、B 事件不会提交或关闭。

它不是完整的 Combobox：不接受字符输入，也不提供文本过滤。需要从固定
选项中使用手柄选择时使用 `SelectBox`；需要搜索大量选项时，应另行实现
可输入过滤的 Combobox。

## 布局模型

`block` 沿垂直方向依次排列子元素，`inline` 沿水平方向依次排列子元素。`stack` 将所有子元素放置在同一个内容区域。

这里的 `block` 和 `inline` 描述的是容器对其直接子节点采用的内部布局方式，并不等同于浏览器 CSS 中同名 display 类型的完整语义：

- `block` 约束子节点纵向排列；
- `inline` 约束子节点横向排列；
- 一个节点自身在父容器中使用多大空间，由父容器调用其 `Calc` 方法时传入的 `Constraints` 决定；
- 当前节点自己的 `block` 或 `inline` 类型，只影响它如何排列下一层子节点。

例如，Inline 会横向放置一个 Block，但这个 Block 仍会纵向放置自己的子节点：

```html
<inline>
    <block>
        <text>第一行</text>
        <text>第二行</text>
    </block>
</inline>
```

Go 布局接口中的尺寸偏好含义如下：

| 字段 | `true` | `false` |
| --- | --- | --- |
| `PrefersMaxWidth` | 当前节点优先使用父节点提供的全部可用宽度 | 当前节点按内容所需宽度收缩 |
| `PrefersMaxHeight` | 当前节点优先使用父节点提供的全部可用高度 | 当前节点按内容所需高度收缩 |

`Constraints` 约束的是正在执行 `Calc` 的节点自身，而不是它的子节点排列方向。显式设置的 `width`、`height` 优先于相应的 `PrefersMax*` 尺寸偏好。

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

## 系统覆盖层和安全区域

状态栏等系统界面可以作为独立覆盖层，始终绘制在所有普通文档之上。普通文档仍使用完整屏幕，因此背景图片可以延伸至屏幕边缘；需要避免被状态栏遮挡的内容放入 `<safe-area>`。

覆盖层文档使用以下固定 ID 声明四边占用区域：

- `#top` 和 `#bottom` 的布局高度分别作为顶部、底部 inset；
- `#left` 和 `#right` 的布局宽度分别作为左侧、右侧 inset；
- 缺少某个元素时，对应 inset 为零。

例如 `status.html`：

```html
<document>
<stack fill>
    <block>
        <inline id="top" height="48" background-color="#000000E8">
            <text>状态栏</text>
        </inline>
        <spacer></spacer>
    </block>
</stack>
</document>
```

创建并设置覆盖层：

```go
overlay := app.NewOverlay(assets, "status.html")
app.SetOverlay(overlay)
```

普通文档可以把全屏背景和安全内容叠放：

```html
<document>
<stack fill>
    <img src="background.png" fill="stretch">

    <safe-area fill>
        <block fill>
            <text>不会被系统覆盖层遮挡</text>
        </block>
    </safe-area>
</stack>
</document>
```

`<safe-area>` 会在布局时自动采用当前四边 inset。覆盖层尺寸改变、被替换或被移除时，显示中的普通文档会重新布局。调用下面任一种方式可以移除覆盖层：

```go
app.SetOverlay(nil)
// 或
overlay.Close()
```

覆盖层仅负责顶层绘制，不会加入普通文档栈，也不会成为接收按键事件的活动文档。`<safe-area>` 的 padding 由系统占用区域管理，不应另外设置 `padding`；需要额外留白时，在其内部再放置带 padding 的容器。

完整示例见 [`demo/safe`](demo/safe)。

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
- `fill`

`width` 和 `height` 可以解析整数或百分比。百分比布局目前仍有已知限制，参见 [`todo.md`](todo.md)。

`padding` 接受一至四个 `0...65535` 范围内的整数，展开顺序与 CSS shorthand 相同：

```css
padding: 10;          /* 10 10 10 10 */
padding: 10 20;       /* 10 20 10 20 */
padding: 10 20 30;    /* 10 20 30 20 */
padding: 10 20 30 40; /* top right bottom left */
```

`font-size` 接受整数、百分比、非负 `rem` 和命名字号 `xx-small`、`x-small`、`small`、`medium`、`large`、`x-large`、`xx-large`。百分比相对于父元素的计算字号，`rem` 相对于 `<document>` 的计算字号：

```css
document { font-size: 32; }
.title { font-size: 1.5rem; } /* 48 */
.hint { font-size: 75%; }    /* 父元素计算字号的 75% */
```

`fill` 当前可用值为 `stretch`、`contain` 和 `scale-down`；`cover` 与 `none` 尚未支持。

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

选择器可以在规则内嵌套。未使用 `&` 时，嵌套选择器默认匹配父选择器的后代；
`&` 表示父选择器本身，前导 `>` 则表示直接子元素：

```css
.card {
    color: white;

    .title {                 /* .card .title */
        font-size: 32;
    }

    &.selected {             /* .card.selected */
        outline-width: 3;
    }

    > .icon {                /* .card > .icon */
        width: 24;
    }
}
```

支持多层嵌套和逗号分组；父子都是分组选择器时会展开为所有组合。
Nesting 仍只能使用上述选择器子集，不支持伪类、属性选择器、兄弟选择器或媒体规则。

样式来源的覆盖顺序为：

```text
默认样式 < 文档样式表 < 元素内联属性
```

同一来源内先比较选择器 specificity；specificity 相同时，源码中靠后的声明优先。

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
})

defer remove()
box.Activate()
```

捕获阶段监听器：

```go
doc.ListenOptions(fbiw.StickDownEvent, func(event *fbiw.Event) {
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
    func() (fbiw.Box, *Item) {
        item := fbiw.Unmarshal[Item](doc, `<block><text></text></block>`)
        return item.root, item
    },
    func(item *Item, index int) {
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

### Scroll 的行数与行高

`rows` 和 `max-rows` 都表示可视槽位的行数，但语义不同：

- `rows`：固定显示区域的行数。即使数据不足，`Scroll` 也不因数据量而收缩。
- `max-rows`：最多显示的行数。数据不足时按实际行数收缩，超过上限后滚动。
- `row-height`：每行槽位的固定高度，不包含 `gap`。

`rows` 与 `max-rows` 互斥，一个 `Scroll` 只能指定其中一个。两者分别与可选的
`row-height` 组合，共有以下四种布局方式：

| 配置 | 槽位高度 | Scroll 高度 | 数据不足时 | 数据超出时 |
| --- | --- | --- | --- | --- |
| `rows` | 从可用高度均分 | 保持外部提供的高度 | 保持固定高度，留下空槽位 | 在固定槽位中滚动 |
| `rows` + `row-height` | 使用 `row-height` | 仍由外部高度决定 | 保持固定高度，留下空槽位 | 在固定槽位中滚动 |
| `max-rows` | 按满额时的可用高度均分 | 按实际数据行数收缩，满额时等于外部提供的最大高度 | 收缩 | 保持最大高度并滚动 |
| `max-rows` + `row-height` | 使用 `row-height` | 根据实际可见行数计算，最多为 `max-rows` 行 | 收缩 | 保持最大高度并滚动 |

其中，数据实际占用的行数为：

```text
dataRows = ceil(count / cols)
```

#### 1. `rows`：固定行数，自动均分行高

```html
<scroll rows="5" height="196" gap="4"></scroll>
```

去掉 padding、border 和四个 `gap` 后，剩余高度平均分给五行。数据只有一项时，
容器仍然保持五行区域的高度；数据超过五行时开始滚动。

#### 2. `rows` + `row-height`：固定行数和固定槽位高度

```html
<scroll rows="5" row-height="36" height="196" gap="4"></scroll>
```

每个槽位固定为 36 像素，`Scroll` 本身仍然使用外部提供的高度。调用者应保证容器
内容区的高度与所有槽位和间距匹配：

```text
contentHeight = rows * rowHeight + (rows - 1) * gap
```

如果外部高度更大，会留下额外空间；如果更小，槽位可能超出容器。这个组合适用于
弹窗尺寸由外层统一控制、但列表项必须保持固定高度的场景。

#### 3. `max-rows`：限定最大行数，自动均分行高

```html
<scroll max-rows="5" height="196" gap="4"></scroll>
```

外部提供的高度表示五行满额时的最大高度。槽位高度按五行均分得到；只有两行数据时，
`Scroll` 收缩为两行槽位加一个 `gap` 的高度。达到或超过五行后保持最大高度并滚动。

#### 4. `max-rows` + `row-height`：限定最大行数和固定槽位高度

弹出菜单等项目数量不固定的场景，推荐使用这个组合：

```html
<scroll max-rows="5" row-height="36" gap="4"></scroll>
```

实际高度按可见数据行数计算：

```text
visibleRows = min(dataRows, maxRows)
contentHeight = visibleRows * rowHeight + max(visibleRows - 1, 0) * gap
scrollHeight = border + padding + contentHeight
```

例如 `max-rows="5" row-height="36" gap="4"`：一行数据的内容高度是 36，三行是
116，五行及更多数据是 196；第六行开始通过滚动访问。空列表不产生槽位和 `gap`，
高度只包含 border 和 padding。

## 图片和字体

相对图片路径从创建文档时传入的文件系统中读取：

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
app := fbiw.NewApp(
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

仓库包含多个示例，其中安全区域示例可这样运行：

```bash
GOEXPERIMENT=simd go run ./demo
GOEXPERIMENT=simd go run ./demo/scroll
GOEXPERIMENT=simd go run ./demo/safe
```

示例期望存在 `demo/regular.ttf`。该字体文件当前未包含在仓库中，运行前需要自行放置一个可用的 OpenType/TrueType 字体，并命名为 `regular.ttf`。

## 测试

运行全部测试：

```bash
GOEXPERIMENT=simd go test ./...
```

测试目前覆盖样式解析、样式覆盖与继承、相对字号、选择器查询、布局计算以及结构体绑定。平台后端、完整事件循环和异步图片加载尚缺少集成测试。

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
