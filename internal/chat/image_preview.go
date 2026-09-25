package chat

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const PreviewPixelLimit = 1_000_000

// PreviewPath identifies a cached dashboard image without relying on the
// attachment's temporary path. Callers must validate URL-supplied IDs first.
func PreviewPath(cacheDir, sessionID, messageID string, index int) string {
	return filepath.Join(cacheDir, "previews", sessionID, fmt.Sprintf("%s-%d", messageID, index))
}

// PreviewDimensions reports the size the dashboard will render. DecodeConfig
// reads image metadata without decoding the full base64 image.
func PreviewDimensions(img Image) (int, int, error) {
	cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(img.Data)))
	if err != nil {
		return 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, fmt.Errorf("invalid image dimensions %dx%d", cfg.Width, cfg.Height)
	}
	width, height := cappedDimensions(cfg.Width, cfg.Height)
	return width, height, nil
}

func cappedDimensions(width, height int) (int, int) {
	if float64(width)*float64(height) <= PreviewPixelLimit {
		return width, height
	}
	scale := math.Sqrt(PreviewPixelLimit / (float64(width) * float64(height)))
	width, height = max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
	if float64(width)*float64(height) > PreviewPixelLimit {
		if width > height {
			width = PreviewPixelLimit / height
		} else {
			height = PreviewPixelLimit / width
		}
	}
	return width, height
}

// EnsurePreview writes an image once to the workspace cache. Small images keep
// their original encoding; larger images become PNGs capped at one megapixel.
func EnsurePreview(cacheDir, sessionID, messageID string, index int, img Image) error {
	path := PreviewPath(cacheDir, sessionID, messageID, index)
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		if f, err := os.Open(path); err == nil {
			cfg, _, decodeErr := image.DecodeConfig(f)
			f.Close()
			if decodeErr == nil && cfg.Width > 0 && cfg.Height > 0 && float64(cfg.Width)*float64(cfg.Height) <= PreviewPixelLimit {
				return nil
			}
		}
	}
	cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(img.Data)))
	if err != nil {
		return fmt.Errorf("preview dimensions: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", cfg.Width, cfg.Height)
	}
	width, height := cappedDimensions(cfg.Width, cfg.Height)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".preview-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(img.Data))
	if cfg.Width == width && cfg.Height == height {
		if _, err := io.Copy(f, reader); err != nil {
			return err
		}
	} else {
		src, _, err := image.Decode(reader)
		if err != nil {
			return err
		}
		if err := png.Encode(f, shrinkImage(src, width, height)); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// shrinkImage averages source pixels in each destination pixel's rectangle.
// It preserves the aspect ratio and keeps screenshot text legible at the cap.
func shrinkImage(src image.Image, width, height int) *image.RGBA {
	bounds := src.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		y0, y1 := y*sourceHeight/height, (y+1)*sourceHeight/height
		for x := 0; x < width; x++ {
			x0, x1 := x*sourceWidth/width, (x+1)*sourceWidth/width
			var red, green, blue, alpha uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					red += uint64(r)
					green += uint64(g)
					blue += uint64(b)
					alpha += uint64(a)
				}
			}
			count := uint64((x1 - x0) * (y1 - y0))
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(red / count >> 8), G: uint8(green / count >> 8),
				B: uint8(blue / count >> 8), A: uint8(alpha / count >> 8),
			})
		}
	}
	return dst
}
