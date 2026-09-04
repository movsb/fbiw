# fbiw

`fbiw` 是一个使用 Go 编写的轻量级 GUI 框架，主要面向使用 Linux framebuffer 和游戏手柄按键交互的固定屏幕设备。

它不依赖浏览器或 WebView，而是自行实现了一套精简的 HTML/CSS 风格界面描述、DOM、布局、事件传播和软件渲染系统。在 macOS 上，项目通过 SDL2 提供一个 `1024×768` 的开发窗口，便于在桌面环境中调试界面。

> 项目目前仍处于开发阶段，适合固定分辨率、方向键操作的掌机菜单和系统界面，不应视为完整的浏览器布局引擎或通用桌面 GUI 框架。

## 特性

- 使用类似 HTML 的文档描述界面；
- 支持标签、ID、class、后代和直接子元素等 CSS 选择器；
- 内置纵向、横向、单行 Flex、叠层和虚拟列表布局；
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
- 内容根节点只能有一个，且必须是 `<block>`、`<inline>`、`<stack>` 或 `<flex>`；
- 普通容器中不能直接放置非空文本，文字必须放在 `<text>` 中；
- `<b>` 和 `<i>` 只能出现在 `<text>`、`<b>` 或 `<i>` 内；
- `<img>` 和 `<spacer>` 是无子节点元素；
- 解析使用 Go 的 HTML5 parser，自定义标签不要使用 `<spacer/>` 形式，应写成 `<spacer></spacer>`。

## 内置组件

| 标签 | 用途 |
| --- | --- |
| `block` | 子元素纵向排列 |
| `inline` | 子元素单行横向排列 |
| `flex` | 子元素单行弹性排列，支持横向或纵向 |
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

Toggle 首次显示时直接呈现当前状态。显示过后切换状态，滑块会在 250ms 内以
`EaseOut` 平滑移动；快速反复切换会取消旧动画，从当前显示位置转向新目标。
`checked`、对应类名和 `OnChange` 立即更新，不等待滑块位置与轨道颜色动画结束。
`SetProp("checked", ...)` 同样更新动效，但仍不派发状态事件。

滑块动画每帧只请求重绘；切换 `checked` 类名本身仍可能因 CSS 规则触发布局。
动画期间尺寸变化会按新的滑动距离绘制。尚未首次绘制时直接更新位置；
后台和 Detach 沿用 Timeline 的暂停回调、时间继续策略，关闭文档自动取消动画。

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

`Value` 和 `SetValue` 操作逻辑进度；设置新值后，显示进度会在 250ms 内以
`EaseOut` 从当前位置追到目标。连续更新会取消旧动画并从当前显示位置继续，
动画帧只请求重绘。首次绘制前直接显示目标值；后台、Detach 和文档关闭行为
与其他 Timeline 动画一致。

不知道完成比例时，可以启用不确定模式：

```html
<progress indeterminate></progress>
```

```go
progress.SetIndeterminate(true)
// 得到实际进度后：
progress.SetValue(0.35)
progress.SetIndeterminate(false)
```

不确定模式显示一个往返移动的色块；它会在轨道两端完全移出并短暂停留，
再从同一侧重新进入。期间 `Value` 与 `SetValue` 仍保存
确定进度，切回确定模式后立即显示该值。循环由组件内部续订普通 `Animate`，
端点停留期间停止请求动画帧；整个循环不会在动画公共接口中引入 repeat。

标题、两端数值和百分比文字由外部元素提供。两端文字可以和 Progress
一起放在 `block` 中，中间覆盖的百分比可以使用 `stack` 将文字叠在
Progress 上方。默认尺寸为 `8em × 0.5em`，也可以通过 CSS 覆盖尺寸、
padding、背景和边框。

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

这里的 `block` 和 `inline` 标签描述的是容器对其直接子节点采用的内部布局方式。布局类型由盒子类型决定，`display` 只控制显示/隐藏，并不等同于浏览器 CSS 的 display 属性：

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

`Constraints` 约束的是正在执行 `Calc` 的节点自身，而不是它的子节点排列方向。尺寸优先级为 `FixedWidth/FixedHeight` > 样式 `width/height` > `PrefersMax*` 尺寸偏好或内容尺寸。`FixedWidth/FixedHeight` 使用 `NumberLength(n)` 表示父布局分配的最终 border-box 尺寸，空值表示不强制；不得将分配结果写回样式。自定义 `Calc` 也应遵守这个约定。

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

`block/inline` 保持原来的布局规则；`inline` 不会自动换行，也没有通用 margin、min/max size、绝对定位或通用 overflow 裁剪。

### 单行 Flex

使用 `<flex>`（Go 构造函数为 `NewFlex(doc)`）创建单行弹性布局容器，也可作为文档内容根节点。自身如何参与上一层布局仍由父容器决定。布局类型不能通过 `display` 改变；原来的 `display="flex"` 写法需要改为 `<flex>` 标签。

```html
<flex width="300" height="80" gap="12" align-items="center">
    <block width="60" height="40" background-color="#3358d4"></block>
    <block width="0" height="40" flex-grow="1" background-color="#34c759"></block>
    <block width="0" height="40" flex-grow="2" background-color="#f5a623"></block>
</flex>
```

扣除固定宽度 60 和两个间距 24 后，剩余 216 按 1:2 分配，后两个元素宽度分别为 72、144。

| 属性 | 默认值 | 支持值 |
| --- | --- | --- |
| `flex-direction` | `row` | `row`、`column` |
| `flex-grow` | `0` | 非负有限数值，作用于 Flex 的直接子元素 |
| `gap` | `0` | 非负整数像素，仅在可见子元素之间留间距 |
| `justify-content` | `start` | `start`、`end`、`center`、`space-between`、`space-around`、`space-evenly` |
| `align-items` | `stretch` | `start`、`end`、`center`、`stretch` |
| `align-self` | `auto` | `auto` 或上述 `align-items` 值；`auto` 采用父容器设置 |

对齐属性同时接受 `flex-start` / `flex-end` 别名。这些属性不继承；`align-self` 和 `flex-grow` 由父 Flex 读取。Flex 容器使用新对齐属性，不使用旧的 `align` 排列子元素。

- `flex-grow` 在基础尺寸上增加空间：基础尺寸来自显式宽高、百分比或内容测量。若想按权重分配全部主轴空间，横排设置 `width="0"`，竖排设置 `height="0"`。
- 本项目按正权重比例分完剩余空间，即使权重之和小于 1；这是轻量布局规则，不是完整 CSS Flexbox 算法。
- `stretch` 只拉伸未指定交叉轴尺寸的元素；显式尺寸（包括 0 和百分比）保持不变。
- 文本作为一个 Flex item，可在分配后的宽度内多行断行；这不代表 Flex items 自身支持换行。
- 未实现 `flex-shrink`、`flex-basis`、`flex` shorthand、`flex-wrap`、反向排列、`order`、baseline 和 min/max 尺寸。空间不足时保持基础尺寸并溢出，grow 不会分配负尺寸。
- `<spacer>` 和 `spacer` 属性不会在 Flex 中自动启用增长，需要显式设置 `flex-grow`。`gap` 是 Flex 和 Scroll 共用的非继承样式，分别表示子元素间距和行列槽位间距；Block/Inline 暂不使用它。Scroll 同时支持 `<scroll gap="4">` 和 `scroll { gap: 4; }`，内联属性优先，动态修改会触发重新布局。
- Scroll 等自带内部布局的专用组件可作为 Flex item，但保持各自的内部布局规则。

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
- `flex-direction`、`flex-grow`、`gap`
- `justify-content`、`align-items`、`align-self`

`width` 和 `height` 可以解析整数或百分比。常规 `Calc` 布局每次根据父容器提供的完整内容区解析百分比，不受前序兄弟元素占用影响，也不会改写计算后的样式；根元素以文档尺寸为参考。文本的独立分段路径和内容自适应父容器的百分比规则仍有限制，参见 [`todo.md`](todo.md)。

`display` 是非继承的 bool 样式，默认 `true`。接受 `true` / `false`、`1` / `0`，空属性 `<block display>` 表示 `true`。隐藏元素不参与父布局和绘制，隐藏祖先下的子元素也不会显示。`none`、`block`、`inline`、`flex` 等布局关键字不再接受；隐藏请使用 `display="false"`，选择布局请使用相应的盒子标签。Go 中使用 `Styles.SetDisplay(bool)`，`DisplayMode` 类型已移除。

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

### 主题颜色

样式表的 `color`、`background-color`、`border-color` 和 `outline-color`
可以引用主题中的语义颜色：

```css
button {
    color: var(--color-on-primary);
    background-color: var(--color-primary);
}
```

App 内置浅色和深色主题；即使不传任何主题选项，也会随本地时间自动切换。
`defaults.css` 中的按钮、选择框等控件颜色均来自内置主题。内置主题提供以下常用变量：

```text
--color-text              --color-background
--color-surface           --color-muted
--color-border            --color-focus
--color-primary           --color-on-primary
--color-destructive       --color-on-destructive
```

自定义主题会覆盖对应时段的内置主题，因此只需提供想修改的颜色；未提供的变量继续使用
系统浅色或深色主题中的值。
预置色值分别维护在 `assets/light.css` 和 `assets/dark.css` 中，使用
`:root { --color-name: value; }` 格式；这里的 `:root` 是主题文件专用语法，不会扩展
普通样式表的选择器子集。

应用创建时可以设置初始主题，之后切换主题会自动重新计算所有文档样式并重绘：

```go
app := fbiw.NewApp(
    fbiw.WithTheme("light", themeFS, "light.css"),
    fbiw.WithTheme("dark", themeFS, "dark.css"),
)
if err := app.SetThemeLight("light"); err != nil {
    panic(err)
}
if err := app.SetThemeDark("dark"); err != nil {
    panic(err)
}
```

主题由 App 统一管理。本地时间 06:00 至 18:00 使用浅色主题，18:00 至次日
06:00 使用深色主题。App 每分钟检查一次本地时间，系统休眠或时钟变更后也会在恢复运行后约一分钟内校正。
只配置一种时会始终使用该主题。
切换前会检查所有文档引用的颜色；缺失颜色时返回错误并保留旧主题。

当前仅支持 `<style>` 中的 `var(--color-*)`，不支持 fallback、非颜色变量或元素内联属性中的主题变量。

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

`Document.Unmarshal` 可用于动态创建一段组件树：

```go
type Item struct {
    root fbiw.Box
    text *fbiw.Text `css:"text"`
}

item := doc.Unmarshal[Item](`
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
<template id="item">
    <block><text></text></block>
</template>

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
        item := doc.Instantiate[Item]("item")
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

## 动画帧时钟

`Document.RequestAnimationFrame(func(now time.Time))` 请求一次动画帧回调，
返回可重复调用的取消函数。持续动画需要在回调中再次申请：

```go
var cancelFrame func()
var started time.Time
var frame func(time.Time)
frame = func(now time.Time) {
    if started.IsZero() {
        started = now
    }
    elapsed := now.Sub(started)
    // 根据 elapsed 更新组件状态；需要时调用 RequestPaint/RequestLayout。
    if elapsed < time.Second {
        cancelFrame = doc.RequestAnimationFrame(frame)
    }
}
cancelFrame = doc.RequestAnimationFrame(frame)

// 在需要提前停止时调用（UI 主线程）：
// cancelFrame()
```

所有文档共享 App 的 60 FPS 目标时钟，同一帧回调按注册顺序执行，获得相同的
`time.Time`（保留单调时钟读数），然后统一检查布局和绘制需求。实际帧率取决于
绘制耗时及平台后端；卡顿只跳到当前时间，不补发历史帧。回调本身不会自动重绘。

注册、取消和回调均属于 UI 主线程；其他 goroutine 请通过 `Document.Async` 投递。
回调内注册的请求最早下一帧执行；取消也能阻止本帧中尚未开始的回调。
nil 回调、未绑定或已关闭文档的请求会 panic。

前台 Desktop 的所有文档与当前挂载的 Overlay 可以执行回调，不做遮挡或元素级
可见性判断。后台桌面、未挂载 Overlay 和 App Detach 期间保留请求但暂停回调，
时间仍继续流逝；恢复后收到当前时间。无可运行请求时，动画时钟不产生周期唤醒
（不改变 macOS 原有事件轮询方式）。关闭文档自动取消其请求，App 退出时清理全部请求。

## 进度动画与数值补间

`Document.Animate` 提供经过缓动的 `0..1` 时间进度，适合在一次回调中更新颜色、
位置等一个或多个值：

```go
cancelAnimation := doc.Animate(fbiw.AnimationOptions{
    Duration: 300 * time.Millisecond,
    Easing:   fbiw.EaseOut,
    OnUpdate: func(progress float64) {
        // 使用 progress 插值所需状态，并按需请求布局或重绘。
        doc.RequestPaint()
    },
})

// cancelAnimation()
```

`Animate` 只负责时间和进度，不规定被更新的值类型。`NumberAnimator` 和
`ColorAnimator` 根据起止值创建插值函数，因此多个值可以共享一次动画：

```go
move := fbiw.NumberAnimator(oldX, newX)
fade := fbiw.ColorAnimator(oldColor, newColor)

cancelAnimation := doc.Animate(fbiw.AnimationOptions{
    Duration: 300 * time.Millisecond,
    Easing:   fbiw.EaseOut,
    OnUpdate: func(progress float64) {
        x = move(progress)
        color = fade(progress)
        doc.RequestPaint()
    },
    OnComplete: func() {
        log.Println("动画完成")
    },
})

// 需要提前停止时在 UI 主线程调用，可重复调用：
// cancelAnimation()
```

颜色逐 RGBA 通道在 sRGB 数值空间插值。`ColorNone` 和 `ColorClear` 具有特殊
绘制语义，不能作为颜色插值器的端点。两个插值器都会把范围外的进度截到端点，
并拒绝 `NaN` 进度。

- 从调用时开始计时，回调不会同步执行；首帧按实际经过时间计算，不保证恰好交付
  `From`。需要立即显示起点时，应先设置组件状态。帧回调内创建的 Animate 使用本帧统一时间戳作为起点。
- 默认 `EaseLinear` 为匀速；`EaseIn`、`EaseOut`、`EaseInOut` 使用二次曲线，
  不等同于 CSS 同名关键字的三次贝塞尔曲线。
- `OnUpdate` 必填，时长不能为负。零时长在下一帧直接交付进度 1。
- 自然结束时先精确交付进度 1，再调用可选的 `OnComplete`，且只完成一次。
  取消、关闭文档或退出 App 不触发完成回调；在最后一次 `OnUpdate` 中取消也会阻止它。
- 后台和 Detach 行为沿用帧时钟：暂停回调，不暂停时间，恢复后追上当前进度或直接完成。
- Animate 不自动修改样式或标记重绘。动画中途改变目标时，先取消旧动画，
  再以当前显示值创建新的插值器，避免跳变。

### 文档 Timeline

每个使用 Animate 的文档会按需创建一个内部 Timeline，统一管理活动动画：

- 同一文档的所有动画共享一个帧请求，按文档内的注册顺序推进。
- 完成或取消的动画会被移除；没有活动动画时停止续订，后续可以复用该 Timeline。
- 帧回调中新增的动画最早下一帧执行，即使它所属的 Timeline 在本帧还未执行。
- 关闭文档会清理整个 Timeline；App 退出时也会清理后台文档的活动动画。

时钟只负责帧调度，Timeline 负责集合与续订，Animate 负责时间进度，
Animator 负责具体值的插值。直接使用 `RequestAnimationFrame`
的回调仍是独立请求；与这些动画混用时，Timeline 中的动画按文档成批推进，
不保证它们与独立帧回调交错的注册顺序。

目前不提供 CSS Transition、循环、倍速或倒放；Toggle 使用一个进度同时完成
滑块位置和轨道颜色动画，其他组件尚未自动添加动效。

## 异步更新

UI 修改应在主事件线程执行。从其他 goroutine 更新界面时，可使用：

```go
app.Async(func() {
    text.SetText("加载完成")
})
```

文档相关任务优先使用 `Document.Async`。它会绑定提交时的文档生命周期：

```go
go func() {
    result := loadData()
    doc.Async(func() {
        // 文档仍然有效时，才会在 UI 主线程执行。
        render(result)
    })
}()
```

文档在提交前未绑定、App 已退出，或者回调执行前已关闭或解绑时，回调会被忽略。
判断发生在 UI 回调真正执行前，而不是后台任务完成时。`Document.Async` 可以从其他
goroutine 调用，nil 回调会 panic。通用且不属于某个文档的任务继续使用 `App.Async`。

仅需重绘时调用 `RequestPaint`，尺寸或结构变化时调用 `RequestLayout`。对应的
`RequestPaintAsync` 和 `RequestLayoutAsync` 复用生命周期绑定的投递，文档失效后自动忽略。

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
GOEXPERIMENT=simd go run ./demo/theme
```

示例期望存在 `demo/regular.ttf`。该字体文件当前未包含在仓库中，运行前需要自行放置一个可用的 OpenType/TrueType 字体，并命名为 `regular.ttf`。

Flex 交互示例（使用 `<flex>`，不使用 `display` 切换布局）：

```bash
cd demo/flex
GOEXPERIMENT=simd go run .
```

它从 `../regular.ttf` 加载字体，展示横向 grow 分配、竖向 1:2 分配、主轴/交叉轴对齐和文本自动折行。左右调整中间元素的 grow（1～5），上下切换主轴对齐，A 切换交叉轴对齐，B 隐藏/显示橙色元素。在 macOS 上对应 A/D、W/S、K、J 键。

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
