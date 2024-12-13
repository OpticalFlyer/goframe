// main.go
package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/draw"
)

type Photo struct {
	Path string
	Img  *ebiten.Image
}

type Game struct {
	photos       []Photo
	currentIdx   int
	lastUpdate   time.Time
	mu           sync.RWMutex
	paused       bool
	overlayStart time.Time
	showOverlay  bool
	photoDir     string
}

var whiteImage = ebiten.NewImage(3, 3)

func init() {
	whiteImage.Fill(color.White)
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

func (g *Game) drawOverlay(screen *ebiten.Image, width, height int) {
	if !g.showOverlay {
		return
	}

	elapsed := time.Since(g.overlayStart).Seconds()
	if elapsed > 1.0 {
		g.showOverlay = false
		return
	}

	alpha := float32(1.0 - elapsed)
	centerX := float32(width / 2)
	centerY := float32(height / 2)

	var path vector.Path

	if g.paused {
		// Draw pause bars
		barWidth := float32(20)
		barHeight := float32(60)
		gap := float32(20)

		// Left bar
		path.MoveTo(centerX-gap-barWidth, centerY-barHeight/2)
		path.LineTo(centerX-gap, centerY-barHeight/2)
		path.LineTo(centerX-gap, centerY+barHeight/2)
		path.LineTo(centerX-gap-barWidth, centerY+barHeight/2)
		path.Close()

		// Right bar
		path.MoveTo(centerX+gap, centerY-barHeight/2)
		path.LineTo(centerX+gap+barWidth, centerY-barHeight/2)
		path.LineTo(centerX+gap+barWidth, centerY+barHeight/2)
		path.LineTo(centerX+gap, centerY+barHeight/2)
		path.Close()
	} else {
		// Draw play triangle
		size := float32(30)
		path.MoveTo(centerX-size, centerY-size)
		path.LineTo(centerX-size, centerY+size)
		path.LineTo(centerX+size, centerY)
		path.Close()
	}

	// Get vertices and indices
	vertices, indices := path.AppendVerticesAndIndicesForFilling(nil, nil)

	// Set colors for all vertices
	for i := range vertices {
		vertices[i].SrcX = 1
		vertices[i].SrcY = 1
		if g.paused {
			vertices[i].ColorR = 1
			vertices[i].ColorG = 0
			vertices[i].ColorB = 0
		} else {
			vertices[i].ColorR = 0
			vertices[i].ColorG = 1
			vertices[i].ColorB = 0
		}
		vertices[i].ColorA = alpha
	}

	// Draw the shape
	op := &ebiten.DrawTrianglesOptions{
		FillRule: ebiten.FillRuleNonZero,
	}
	screen.DrawTriangles(vertices, indices, whiteImage, op)
}

func (g *Game) handleInput(x, screenWidth int) {
	third := screenWidth / 3

	if x < third { // Left third
		g.previousPhoto()
	} else if x > third*2 { // Right third
		g.nextPhoto()
	} else { // Center third
		g.paused = !g.paused
		g.showOverlay = true
		g.overlayStart = time.Now()
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

	if ebiten.IsFullscreen() {
		ebiten.SetCursorMode(ebiten.CursorModeHidden)
	} else {
		ebiten.SetCursorMode(ebiten.CursorModeVisible)
	}

	return nil
}

func (g *Game) nextPhoto() {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.photos) > 0 {
		g.currentIdx = (g.currentIdx + 1) % len(g.photos)
	}
	g.lastUpdate = time.Now()
}

func (g *Game) previousPhoto() {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.photos) > 0 {
		g.currentIdx--
		if g.currentIdx < 0 {
			g.currentIdx = len(g.photos) - 1
		}
	}
	g.lastUpdate = time.Now()
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Clear()

	g.mu.RLock()
	if len(g.photos) == 0 {
		g.mu.RUnlock()
		return
	}
	img := g.photos[g.currentIdx].Img
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

	// Draw overlay after image
	width, height := screen.Bounds().Dx(), screen.Bounds().Dy()
	g.drawOverlay(screen, width, height)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

func (g *Game) AddImage(path string, img *ebiten.Image) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Check if image already exists
	for _, photo := range g.photos {
		if photo.Path == path {
			return // Image already exists
		}
	}
	g.photos = append(g.photos, Photo{Path: path, Img: img})
	fmt.Printf("Added image: %s\n", path)
}

func (g *Game) RemoveImageByPath(path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := -1
	for i, photo := range g.photos {
		if photo.Path == path {
			index = i
			break
		}
	}

	if index == -1 {
		fmt.Printf("Image not found for removal: %s\n", path)
		return
	}

	// Remove the image from the slice
	g.photos = append(g.photos[:index], g.photos[index+1:]...)
	fmt.Printf("Removed image: %s\n", path)

	// Adjust currentIdx if necessary
	if g.currentIdx >= len(g.photos) && len(g.photos) > 0 {
		g.currentIdx = 0
	}
}

func loadImagesFromDir(dir string, game *Game) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	semaphore := make(chan struct{}, 4)

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".jpeg" || filepath.Ext(file.Name()) == ".jpg" {
			semaphore <- struct{}{}
			go func(filename string) {
				defer func() { <-semaphore }()

				path := filepath.Join(dir, filename)
				img, err := loadImage(path)
				if err != nil {
					fmt.Printf("Failed to load image %s: %v\n", path, err)
					return
				}
				game.AddImage(path, img)
			}(file.Name())
		}
	}

	fmt.Println("Started loading images asynchronously")
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

func watchDirectory(dir string, game *Game) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Printf("Failed to create watcher: %v\n", err)
		return
	}
	defer watcher.Close()

	err = watcher.Add(dir)
	if err != nil {
		fmt.Printf("Failed to add directory to watcher: %v\n", err)
		return
	}

	fmt.Printf("Started watching directory: %s\n", dir)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Handle different event types
			switch {
			case event.Op&fsnotify.Create == fsnotify.Create:
				if filepath.Ext(event.Name) == ".jpeg" || filepath.Ext(event.Name) == ".jpg" {
					fmt.Printf("Detected new image: %s\n", event.Name)
					go func(path string) {
						img, err := loadImage(path)
						if err != nil {
							fmt.Printf("Failed to load new image %s: %v\n", path, err)
							return
						}
						game.AddImage(path, img)
					}(event.Name)
				}
			case event.Op&fsnotify.Remove == fsnotify.Remove, event.Op&fsnotify.Rename == fsnotify.Rename:
				if filepath.Ext(event.Name) == ".jpeg" || filepath.Ext(event.Name) == ".jpg" {
					fmt.Printf("Detected removed image: %s\n", event.Name)
					game.RemoveImageByPath(event.Name)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("Watcher error: %v\n", err)
		}
	}
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get user home directory: %v\n", err)
		return
	}

	dir := filepath.Join(homeDir, ".goframe")

	// Create Game instance
	game := &Game{
		photos:     make([]Photo, 0),
		currentIdx: 0,
		lastUpdate: time.Now(),
		paused:     false,
		photoDir:   dir,
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Failed to create directory: %v\n", err)
		return
	}

	// Start loading images asynchronously
	if err := loadImagesFromDir(dir, game); err != nil {
		fmt.Printf("Failed to start loading images: %v\n", err)
		return
	}

	// Start directory watcher
	go watchDirectory(dir, game)

	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowSize(800, 600)
	ebiten.SetWindowTitle("Photo Frame")
	// ebiten.SetFullscreen(true)

	if err := ebiten.RunGame(game); err != nil {
		fmt.Printf("Failed to run game: %v\n", err)
	}
}
