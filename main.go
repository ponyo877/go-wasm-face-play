//go:build js && wasm
// +build js,wasm

package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"syscall/js"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

var (
	video  js.Value
	stream js.Value
	canvas js.Value
	ctx    js.Value
)

const (
	ScreenWidth  = 320
	ScreenHeight = 240
	buttonWidth  = 100
	buttonHeight = 50
)

func init() {
	doc := js.Global().Get("document")
	video = doc.Call("createElement", "video")
	canvas = doc.Call("createElement", "canvas")
	video.Set("autoplay", true)
	video.Set("muted", true)
	video.Set("videoWidth", ScreenWidth)
	video.Set("videoHeight", ScreenHeight)
	mediaDevices := js.Global().Get("navigator").Get("mediaDevices")
	promise := mediaDevices.Call("getUserMedia", map[string]interface{}{
		"video": true,
		"audio": false,
	})
	promise.Call("then", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		stream = args[0]
		video.Set("srcObject", stream)
		video.Call("play")
		canvas.Set("width", ScreenWidth)
		canvas.Set("height", ScreenHeight)
		ctx = canvas.Call("getContext", "2d")
		return nil
	}))
}

func fetchVideoFrame() []byte {
	ctx.Call("drawImage", video, 0, 0, ScreenWidth, ScreenHeight)
	data := ctx.Call("getImageData", 0, 0, ScreenWidth, ScreenHeight).Get("data")
	jsBin := js.Global().Get("Uint8Array").New(data)
	goBin := make([]byte, data.Get("length").Int())
	_ = js.CopyBytesToGo(goBin, jsBin)
	return goBin
}

type Game struct {
	drawImg *ebiten.Image
}

func newGame() *Game {
	return &Game{
		drawImg: ebiten.NewImage(ScreenWidth, ScreenHeight),
	}
}

func (g *Game) Update() error {
	if !ctx.Truthy() {
		return nil
	}
	goBin := fetchVideoFrame()
	g.drawImg = ebiten.NewImageFromImage(newImage(goBin, ScreenWidth, ScreenHeight))
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.DrawImage(g.drawImg, nil)
	ebitenutil.DebugPrint(screen, fmt.Sprintf("%f", ebiten.ActualFPS()))
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return ScreenWidth * 2, ScreenHeight * 2
}

func main() {
	ebiten.SetWindowSize(ScreenWidth, ScreenHeight)
	ebiten.SetWindowTitle("Face Play")
	if err := ebiten.RunGame(newGame()); err != nil {
		log.Fatal(err)
	}
}

func newImage(data []byte, w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			p := y*w + x
			r := uint8(data[p*4])
			g := uint8(data[p*4+1])
			b := uint8(data[p*4+2])
			a := uint8(data[p*4+2])
			m.Set(x, y, color.RGBA{r, g, b, a})
		}
	}
	return m
}
