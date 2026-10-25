package site

import (
	"embed"
	"io/fs"
)

//go:embed assets/sakuna-bantay-logo.svg assets/favicon/*
var embeddedAssets embed.FS

// embeddedLogo returns the embedded brand logo bytes.
func embeddedLogo() ([]byte, error) {
	return embeddedAssets.ReadFile("assets/sakuna-bantay-logo.svg")
}

// embeddedFavicons returns name->bytes for every embedded favicon file.
func embeddedFavicons() (map[string][]byte, error) {
	out := map[string][]byte{}
	ents, err := fs.ReadDir(embeddedAssets, "assets/favicon")
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := embeddedAssets.ReadFile("assets/favicon/" + e.Name())
		if err != nil {
			return nil, err
		}
		out[e.Name()] = b
	}
	return out, nil
}
