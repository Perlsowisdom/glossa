// Command genassets writes the app and Store icons from the approved Glossa icon in build/icon/:
// the Windows icon (build/windows/icon.ico), build/appicon.png, the MSIX tiles in
// build/msix/Assets and the Store listing logos in docs/store.
//
// The sources in build/icon/ are the user's approved files and are never edited here. A size that
// exists in build/icon/png is copied as is; any other size is scaled down from the 1024 px PNG.
// Wide tiles and splash screens put the icon in the middle of a navy (#0A1E38) background.
//
//	cd tools && go run ./genassets <repo root>
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

// navy is the icon's background colour, used around the icon on wide tiles and splash screens.
var navy = color.NRGBA{0x0A, 0x1E, 0x38, 0xFF}

type icons struct {
	dir   string // build/icon/png
	cache map[int]image.Image
}

// square gives the icon at size x size: the PNG of that size when there is one, else the 1024 px
// PNG scaled with Catmull-Rom.
func (s *icons) square(size int) image.Image {
	if img, ok := s.cache[size]; ok {
		return img
	}
	img, err := readPNG(filepath.Join(s.dir, fmt.Sprintf("glossa-icon-%d.png", size)))
	if os.IsNotExist(err) {
		src, err2 := readPNG(filepath.Join(s.dir, "glossa-icon-1024.png"))
		if err2 != nil {
			log.Fatal(err2)
		}
		dst := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		img = dst
	} else if err != nil {
		log.Fatal(err)
	}
	s.cache[size] = img
	return img
}

// wide puts the icon, iconSize tall, in the middle of a w x h navy background.
func (s *icons) wide(w, h, iconSize int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{navy}, image.Point{}, draw.Src)
	x, y := (w-iconSize)/2, (h-iconSize)/2
	draw.Draw(dst, image.Rect(x, y, x+iconSize, y+iconSize), s.square(iconSize), image.Point{}, draw.Over)
	return dst
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", path)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: genassets <repo root>")
	}
	root := os.Args[1]
	src := filepath.Join(root, "build", "icon")
	s := &icons{dir: filepath.Join(src, "png"), cache: map[int]image.Image{}}

	// The Windows icon is the approved .ico as it is (16 to 256 px, small sizes from the small SVG).
	ico, err := os.ReadFile(filepath.Join(src, "glossa-icon.ico"))
	if err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(root, "build", "windows", "icon.ico"), ico)
	write(filepath.Join(root, "build", "appicon.png"), pngBytes(s.square(512)))

	assets := filepath.Join(root, "build", "msix", "Assets")
	squares := []struct {
		name string
		size int
	}{
		{"StoreLogo", 50}, {"StoreLogo.scale-200", 100},
		{"Square44x44Logo", 44}, {"Square44x44Logo.scale-200", 88},
		{"Square44x44Logo.targetsize-24_altform-unplated", 24},
		{"Square44x44Logo.targetsize-48_altform-unplated", 48},
		{"Square44x44Logo.targetsize-256_altform-unplated", 256},
		{"SmallTile", 71}, {"SmallTile.scale-200", 142},
		{"Square150x150Logo", 150}, {"Square150x150Logo.scale-200", 300},
		{"LargeTile", 310}, {"LargeTile.scale-200", 620},
	}
	for _, a := range squares {
		write(filepath.Join(assets, a.name+".png"), pngBytes(s.square(a.size)))
	}
	wides := []struct {
		name       string
		w, h, icon int
	}{
		{"Wide310x150Logo", 310, 150, 96}, {"Wide310x150Logo.scale-200", 620, 300, 192},
		{"SplashScreen", 620, 300, 150}, {"SplashScreen.scale-200", 1240, 600, 300},
	}
	for _, a := range wides {
		write(filepath.Join(assets, a.name+".png"), pngBytes(s.wide(a.w, a.h, a.icon)))
	}

	// Store listing logos (Partner Center asks for these as uploads). 1080 is the 1024 px PNG
	// scaled up slightly; replace it when a 1080 px or larger export exists.
	write(filepath.Join(root, "docs", "store", "logo-300.png"), pngBytes(s.square(300)))
	write(filepath.Join(root, "docs", "store", "logo-1080.png"), pngBytes(s.square(1080)))
}
