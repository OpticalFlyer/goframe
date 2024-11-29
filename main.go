package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"golang.org/x/image/draw"
)

type Game struct {
	images     []*ebiten.Image
	currentIdx int
	lastUpdate time.Time
	mu         sync.RWMutex
	paused     bool
}

func (g *Game) getScreenDimensions() (int, int) {
	if ebiten.IsFullscreen() {
		// Get the primary monitor dimensions when in fullscreen
		monitor := ebiten.Monitor()
		if monitor != nil {
			return monitor.Size()
		}
	}
	// Fall back to window size if not fullscreen or monitor info unavailable
	return ebiten.WindowSize()
}

func (g *Game) handleInput(x, screenWidth int) {
	third := screenWidth / 3

	if x < third { // Left third
		g.previousPhoto()
	} else if x > third*2 { // Right third
		g.nextPhoto()
	} else { // Center third
		g.paused = !g.paused
	}
}

func (g *Game) Update() error {
	width, _ := g.getScreenDimensions()

	// Handle touch input using AppendTouchIDs
	var touches []ebiten.TouchID
	touches = ebiten.AppendTouchIDs(touches)
	if len(touches) > 0 {
		if inpututil.IsTouchJustReleased(touches[0]) {
			x, _ := ebiten.TouchPosition(touches[0])
			g.handleInput(x, width)
		}
	}

	// Handle mouse input
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		x, _ := ebiten.CursorPosition()
		g.handleInput(x, width)
	}

	if !g.paused && time.Since(g.lastUpdate) > 5*time.Second {
		g.nextPhoto()
	}
	return nil
}

func (g *Game) nextPhoto() {
	g.mu.RLock()
	if len(g.images) > 0 {
		g.currentIdx = (g.currentIdx + 1) % len(g.images)
	}
	g.mu.RUnlock()
	g.lastUpdate = time.Now()
}

func (g *Game) previousPhoto() {
	g.mu.RLock()
	if len(g.images) > 0 {
		g.currentIdx--
		if g.currentIdx < 0 {
			g.currentIdx = len(g.images) - 1
		}
	}
	g.mu.RUnlock()
	g.lastUpdate = time.Now()
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Clear()

	g.mu.RLock()
	if len(g.images) == 0 {
		g.mu.RUnlock()
		return
	}
	img := g.images[g.currentIdx]
	g.mu.RUnlock()

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

func (g *Game) AddImage(img *ebiten.Image) {
	g.mu.Lock()
	g.images = append(g.images, img)
	g.mu.Unlock()
}

func loadImagesFromDir(dir string, game *Game) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4) // Limit concurrent goroutines

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".jpeg" {
			wg.Add(1)
			go func(filename string) {
				defer wg.Done()
				semaphore <- struct{}{}        // Acquire
				defer func() { <-semaphore }() // Release

				path := filepath.Join(dir, filename)
				img, err := loadImage(path)
				if err != nil {
					fmt.Printf("Failed to load image %s: %v\n", path, err)
					return
				}
				game.AddImage(img)
			}(file.Name())
		}
	}

	// Start a goroutine to wait for all images to finish loading
	go func() {
		wg.Wait()
		fmt.Println("All images loaded")
	}()

	return nil
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

	const maxWidth, maxHeight = 1920, 1080
	imgWidth := img.Bounds().Dx()
	imgHeight := img.Bounds().Dy()

	scaleX := float64(maxWidth) / float64(imgWidth)
	scaleY := float64(maxHeight) / float64(imgHeight)
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	newWidth := int(float64(imgWidth) * scale)
	newHeight := int(float64(imgHeight) * scale)

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	return ebiten.NewImageFromImage(dst), nil
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get user home directory: %v\n", err)
		return
	}

	dir := filepath.Join(homeDir, ".goframe")

	game := &Game{
		images:     make([]*ebiten.Image, 0),
		currentIdx: 0,
		lastUpdate: time.Now(),
		paused:     false,
	}

	if err := loadImagesFromDir(dir, game); err != nil {
		fmt.Printf("Failed to start loading images: %v\n", err)
		return
	}

	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowSize(800, 600)
	ebiten.SetWindowTitle("Photo Frame")
	ebiten.SetFullscreen(true)
	ebiten.SetCursorMode(ebiten.CursorModeHidden)

	if err := ebiten.RunGame(game); err != nil {
		fmt.Printf("Failed to run game: %v\n", err)
	}
}
