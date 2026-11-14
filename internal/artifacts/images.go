package artifacts

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func fit(b []byte, w, h int) (int, int) {
	config, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || config.Width == 0 || config.Height == 0 {
		return w, h
	}
	if float64(config.Width)/float64(config.Height) > float64(w)/float64(h) {
		return w, int(float64(w) * float64(config.Height) / float64(config.Width))
	}
	return int(float64(h) * float64(config.Width) / float64(config.Height)), h
}
func picture(id int, name, rid string, px, py, w, h int) string {
	return fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="%s"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`, id, x(name), rid, px, py, w, h)
}
func stringsStyle(style domain.Style, key string) []string {
	switch value := style[key].(type) {
	case []string:
		return value
	case []any:
		out := []string{}
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

// ImageAspect reports a stored image's width/height ratio, 0 when unknown.
func ImageAspect(s *store.Store, path string) float64 {
	file, err := s.Path(path)
	if err != nil {
		return 0
	}
	f, err := os.Open(file)
	if err != nil {
		return 0
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil || config.Height == 0 {
		return 0
	}
	return float64(config.Width) / float64(config.Height)
}
