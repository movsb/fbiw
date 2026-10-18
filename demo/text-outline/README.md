# 文字轮廓

从本目录运行（使用上一级目录的 `regular.ttf`）：

```sh
cd demo/text-outline
GOEXPERIMENT=simd go run .
```

在相同的明暗波纹背景上对比无轮廓、1 px 半透明黑色轮廓和可调轮廓。

- 左 / 右：调整第三组轮廓宽度（0–8 px）。
- A：切换半透明黑、不透明黑、深蓝。
- B：重置为 2 px 半透明黑。

按键使用框架的手柄映射。第三组同时演示从父元素继承轮廓，以及内联子元素 使用 `text-outline-width: 0` 单独关闭。
