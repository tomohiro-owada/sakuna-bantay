package brand

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"
)

//go:embed logo-mark-144.png
var logoMark144 []byte

// LogoMark144PNG returns the embedded copy of brand/favicon/logo-mark-144.png.
func LogoMark144PNG() []byte {
	out := make([]byte, len(logoMark144))
	copy(out, logoMark144)
	return out
}

// LogoMark144Image decodes the embedded mark PNG.
func LogoMark144Image() (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(logoMark144))
	return img, err
}
