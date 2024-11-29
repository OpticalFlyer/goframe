package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

type Game struct {
	images     []*ebiten.Image
	currentIdx int
	lastUpdate time.Time
}

func (g *Game) Update() error {
	if time.Since(g.lastUpdate) > 1*time.Second {
		g.currentIdx = (g.currentIdx + 1) % len(g.images)
		g.lastUpdate = time.Now()
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Clear()

	img := g.images[g.currentIdx]
	imgWidth := img.Bounds().Dx()
	imgHeight := img.Bounds().Dy()
	screenWidth := screen.Bounds().Dx()
	screenHeight := screen.Bounds().Dy()

	scaleX := float64(screenWidth) / float64(imgWidth)
	scaleY := float64(screenHeight) / float64(imgHeight)
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(
		(float64(screenWidth)-float64(imgWidth)*scale)/2,
		(float64(screenHeight)-float64(imgHeight)*scale)/2,
	)

	screen.DrawImage(img, op)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

func loadImagesFromDir(dir string) ([]*ebiten.Image, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var images []*ebiten.Image
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".jpeg" {
			path := filepath.Join(dir, file.Name())
			img, err := loadImage(path)
			if err != nil {
				fmt.Printf("Failed to load image %s: %v\n", path, err)
				continue
			}
			images = append(images, img)
		}
	}
	return images, nil
}

func loadImage(path string) (*ebiten.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	return ebiten.NewImageFromImage(img), nil
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get user home directory: %v\n", err)
		return
	}

	dir := filepath.Join(homeDir, ".goframe")
	images, err := loadImagesFromDir(dir)
	if err != nil {
		fmt.Printf("Failed to load images: %v\n", err)
		return
	}

	if len(images) == 0 {
		fmt.Println("No images found in the directory")
		return
	}

	game := &Game{
		images:     images,
		currentIdx: 0,
		lastUpdate: time.Now(),
	}

	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowSize(800, 600)
	ebiten.SetWindowTitle("Photo Frame")
	ebiten.SetFullscreen(true)
	if err := ebiten.RunGame(game); err != nil {
		fmt.Printf("Failed to run game: %v\n", err)
	}
}
