// Package parserassets exposes the product-reviewed ParserPack files that are
// compiled into the client binaries and also shipped as auditable RPM assets.
package parserassets

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.parserpack.json
var files embed.FS

// Asset is one immutable reviewed ParserPack document.
type Asset struct {
	Name string
	Data []byte
}

// All returns defensive copies of every embedded ParserPack in filename order.
func All() ([]Asset, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded parser assets: %w", err)
	}
	assets := make([]Asset, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".parserpack.json") {
			continue
		}
		data, err := files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded parser asset %s: %w", entry.Name(), err)
		}
		assets = append(assets, Asset{Name: entry.Name(), Data: append([]byte(nil), data...)})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	if len(assets) == 0 {
		return nil, fmt.Errorf("no embedded parser assets")
	}
	return assets, nil
}
