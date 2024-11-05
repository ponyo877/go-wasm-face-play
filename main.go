//go:build js && wasm
// +build js,wasm

package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"io"
	"log"
	"syscall/js"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/ponyo877/go-wasm-face-play/detector"
	"github.com/ponyo877/go-wasm-face-play/img"
)

//go:embed laughing_man_bg_black.mpg
var laughing_man_bg_black_mpg []byte

//go:embed laughing_man_mask.png
var mask []byte

var (
	video  js.Value
	stream js.Value
	canvas js.Value
	ctx    js.Value
	det    *detector.Detector
	lm     *ebiten.Image
)

const (
	ScreenWidth  = 640
	ScreenHeight = 480
)

func init() {
	img, _, err := image.Decode(bytes.NewReader(img.LaughingMan))
	if err != nil {
		log.Fatal(err)
	}
	lm = ebiten.NewImageFromImage(img)
	det = detector.NewDetector()
	if err := det.UnpackCascades(); err != nil {
		log.Fatal(err)
	}
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
	player      *MpegPlayer
	err         error
	drawImg     *ebiten.Image
	faceNum     int
	cx, cy, rad float64
}

func newGame() *Game {
	var in io.ReadSeeker
	in = bytes.NewReader(laughing_man_bg_black_mpg)
	player, err := NewMPEGPlayer(bufio.NewReader(in))
	if err != nil {
		log.Fatal("aaa", err)
	}
	return &Game{
		player:  player,
		drawImg: ebiten.NewImage(ScreenWidth, ScreenHeight),
	}
}

func (g *Game) Update() error {
	if g.err != nil {
		return g.err
	}
	if !ctx.Truthy() {
		return nil
	}
	goBin := fetchVideoFrame()
	pixels := rgbaToGrayscale(goBin, ScreenWidth, ScreenHeight)
	// widht, height が逆
	dets := det.DetectFaces(pixels, ScreenHeight, ScreenWidth)
	g.faceNum = len(dets)
	for i := 0; i < g.faceNum; i++ {
		g.cx, g.cy, g.rad = float64(dets[i][1]), float64(dets[i][0]), float64(dets[i][2])*1.5
	}
	g.drawImg = ebiten.NewImageFromImage(newImage(goBin, ScreenWidth, ScreenHeight))
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	if g.err != nil {
		return
	}
	screen.DrawImage(g.drawImg, nil)
	// op := &ebiten.DrawImageOptions{}
	// mag := g.rad / float64(lm.Bounds().Dx())
	// op.GeoM.Scale(mag, mag)
	// op.GeoM.Translate(-g.rad/2.0, -g.rad/2.0)
	// op.GeoM.Translate(g.cx, g.cy)
	// screen.DrawImage(lm, op)
	if err := g.player.Draw(screen, g.rad, int(g.cx), int(g.cy)); err != nil {
		g.err = err
	}
	if g.faceNum > 0 {
		ebitenutil.DebugPrint(screen, fmt.Sprintf("faceNum: %d\nFPS: %f\nfx: %f, fy: %f", g.faceNum, ebiten.ActualFPS(), g.cx, g.cy))
	}
}

// func (g *Game) Draw(screen *ebiten.Image) {
// 	if g.err != nil {
// 		return
// 	}
// 	if err := g.player.Draw(screen, 1000, 0, 0); err != nil {
// 		g.err = err
// 	}
// 	ebitenutil.DebugPrint(screen, fmt.Sprintf("FPS: %0.2f", ebiten.ActualFPS()))
// }

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return ScreenWidth, ScreenHeight
}

func main() {
	_ = audio.NewContext(48000)
	ebiten.SetWindowSize(ScreenWidth, ScreenHeight)
	ebiten.SetWindowTitle("Face Play")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(newGame()); err != nil {
		log.Fatal(err)
	}
}

func newImage(data []byte, w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		m.Pix[i*4+0] = uint8(data[i*4+0])
		m.Pix[i*4+1] = uint8(data[i*4+1])
		m.Pix[i*4+2] = uint8(data[i*4+2])
		m.Pix[i*4+3] = uint8(data[i*4+3])
	}
	return m
}

func rgbaToGrayscale(data []uint8, w, h int) []uint8 {
	gs := make([]uint8, w*h)
	for i := 0; i < w*h; i++ {
		r := float64(data[i*4+0])
		g := float64(data[i*4+1])
		b := float64(data[i*4+2])
		gs[i] = uint8(0.5 + 0.2126*r + 0.7152*g + 0.0722*b)
	}
	return gs
}
