package brand

import (
	"github.com/tomohiro-owada/sakuna-bantay/go/internal/fonts"
)

// Text styles (aliases to the shared fonts package).
type Style = fonts.Style

const (
	Title    = fonts.Title    // Montserrat ExtraBold
	Semi     = fonts.Semi     // Montserrat SemiBold
	NotoSemi = fonts.NotoSemi // Noto Sans SemiBold
	NotoReg  = fonts.NotoReg  // Noto Sans Regular
)

// Fonts is the shared typeface set (alias to fonts.Fonts).
type Fonts = fonts.Fonts

// LoadFonts loads the brand typefaces from dir (assets/fonts).
// It delegates to the shared fonts package so brand and card renderers
// use identical fallback chains (including the verified CFF NotoSansJP).
func LoadFonts(dir string, verbose func(string, ...any)) *Fonts {
	return fonts.LoadFromDir(dir, verbose)
}

// LoadEmbeddedFonts loads the embedded copies (no directory needed).
func LoadEmbeddedFonts() *Fonts {
	return fonts.LoadEmbedded()
}
