package termimg

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Ref is an image found in a README.
type Ref struct {
	Token string
	URL   string
	Alt   string
}

var (
	reLinkedImg = regexp.MustCompile(`\[!\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)\]\([^)]*\)`)
	reMdImg     = regexp.MustCompile(`!\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	reHTMLImg   = regexp.MustCompile(`(?i)<img\s[^>]*?src\s*=\s*["']([^"']+)["'][^>]*>`)
	reAlt       = regexp.MustCompile(`(?i)alt\s*=\s*["']([^"']*)["']`)
)

// ExtractImages swaps each image in markdown for a marker paragraph that
// Compose later replaces with the drawn image. Badges and SVGs are dropped.
func ExtractImages(md string) (string, []Ref) {
	var refs []Ref
	add := func(url, alt string) string {
		url = strings.TrimSpace(url)
		if url == "" || Skip(url) || !strings.HasPrefix(strings.ToLower(url), "http") {
			return ""
		}
		tok := fmt.Sprintf("RORIMGMARK%dX", len(refs))
		refs = append(refs, Ref{Token: tok, URL: url, Alt: alt})
		return "\n\n" + tok + "\n\n"
	}
	md = reLinkedImg.ReplaceAllStringFunc(md, func(s string) string {
		m := reLinkedImg.FindStringSubmatch(s)
		return add(m[2], m[1])
	})
	md = reMdImg.ReplaceAllStringFunc(md, func(s string) string {
		m := reMdImg.FindStringSubmatch(s)
		return add(m[2], m[1])
	})
	md = reHTMLImg.ReplaceAllStringFunc(md, func(s string) string {
		m := reHTMLImg.FindStringSubmatch(s)
		alt := ""
		if a := reAlt.FindStringSubmatch(s); a != nil {
			alt = a[1]
		}
		return add(m[1], alt)
	})
	return md, refs
}

// Compose puts drawn images (or loading / failure notes) where the markers
// ended up in rendered markdown. Images get at most maxCols x maxRows cells.
func Compose(rendered string, refs []Ref, s *Store, maxCols, maxRows int, loadingGlyph string) string {
	if len(refs) == 0 {
		return rendered
	}
	byTok := make(map[string]Ref, len(refs))
	for _, r := range refs {
		byTok[r.Token] = r
	}
	lines := strings.Split(rendered, "\n")
	out := make([]string, 0, len(lines))
	dim := "\x1b[2m"
	for _, line := range lines {
		plain := ansi.Strip(line)
		i := strings.Index(plain, "RORIMGMARK")
		if i < 0 {
			out = append(out, line)
			continue
		}
		j := strings.IndexByte(plain[i:], 'X')
		if j < 0 {
			out = append(out, line)
			continue
		}
		ref, ok := byTok[plain[i:i+j+1]]
		if !ok {
			out = append(out, line)
			continue
		}
		label := ref.Alt
		if label == "" {
			label = "image"
		}
		switch {
		case s == nil || s.Mode == Off:
			out = append(out, "  "+dim+"[image: "+label+"]\x1b[0m")
		case !s.Has(ref.URL):
			out = append(out, "  "+dim+loadingGlyph+" loading "+label+"…\x1b[0m")
		case s.Err(ref.URL) != nil:
			out = append(out, "  "+dim+"✗ "+label+" ("+s.Err(ref.URL).Error()+")\x1b[0m")
		default:
			w, h, _ := s.Bounds(ref.URL)
			cols, rows := FitSize(w, h, maxCols, maxRows)
			img := s.Render(ref.URL, cols, rows)
			out = append(out, "")
			for _, l := range img {
				out = append(out, "  "+l)
			}
			out = append(out, "")
		}
	}
	return strings.Join(out, "\n")
}
