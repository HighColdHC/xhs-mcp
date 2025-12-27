package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func createIcon(filename string, width, height int) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 填充红色 (小红书风格: #FF2442)
	redColor := color.RGBA{0xFF, 0x24, 0x42, 0xFF}
	draw.Draw(img, img.Bounds(), &image.Uniform{redColor}, image.Point{}, draw.Src)

	// 保存为 PNG 文件
	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	err = png.Encode(file, img)
	if err != nil {
		panic(err)
	}
}

func main() {
	// 创建多个尺寸的图标
	createIcon("icon.png", 256, 256)       // 应用图标
	createIcon("icon_16.png", 16, 16)     // 托盘图标
	createIcon("icon_32.png", 32, 32)     // 托盘图标@2x

	println("Created icon.png (256x256), icon_16.png, icon_32.png")
}
