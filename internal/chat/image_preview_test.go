package chat

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

func TestEnsurePreviewCapsPixelsAndReusesCache(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.RGBA{R: 255, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	img := Image{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(encoded.Bytes())}
	if err := EnsurePreview(cacheDir, "session", "message", 0, img); err != nil {
		t.Fatal(err)
	}
	path := PreviewPath(cacheDir, "session", "message", 0)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	width, height := preview.Bounds().Dx(), preview.Bounds().Dy()
	if width*height > PreviewPixelLimit || width < 1400 || height < 700 || width/2 != height {
		t.Fatalf("preview dimensions %dx%d do not preserve the 2:1 image within one megapixel", width, height)
	}
	r, g, b, _ := preview.At(width/2, height/2).RGBA()
	if r < 0xff00 || g != 0 || b != 0 {
		t.Fatalf("preview lost source color: %x %x %x", r, g, b)
	}
	if err := EnsurePreview(cacheDir, "session", "message", 0, Image{}); err != nil {
		t.Fatalf("cached preview should not need the original: %v", err)
	}
}

func TestPreviewDimensionsCapsVeryWideImages(t *testing.T) {
	width, height := cappedDimensions(2_000_000, 1)
	if width*height > PreviewPixelLimit || height != 1 {
		t.Fatalf("very wide preview exceeds pixel cap: %dx%d", width, height)
	}
}
