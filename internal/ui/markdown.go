package ui

import (
	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }

// markdownStyle builds a README style from the current theme.
func markdownStyle() gansi.StyleConfig {
	t := Current
	cfg := styles.DarkStyleConfig
	if t.Light {
		cfg = styles.LightStyleConfig
	}
	cfg.Document.Color = sp(t.Text)
	cfg.Document.BackgroundColor = nil
	cfg.Paragraph.Color = sp(t.Text)
	cfg.Heading.Color = sp(t.Accent)
	cfg.Heading.Bold = bp(true)
	cfg.H1.Color = sp(t.PillFg)
	cfg.H1.BackgroundColor = sp(t.Accent)
	cfg.H1.Bold = bp(true)
	cfg.H2.Color = sp(t.Accent)
	cfg.H3.Color = sp(t.Sel)
	cfg.H4.Color = sp(t.Sel)
	cfg.H5.Color = sp(t.Muted)
	cfg.H6.Color = sp(t.Muted)
	cfg.Link.Color = sp(t.Info)
	cfg.LinkText.Color = sp(t.Info)
	cfg.LinkText.Bold = bp(true)
	cfg.Strong.Color = sp(t.Text)
	cfg.Emph.Color = sp(t.Text)
	cfg.Item.Color = sp(t.Text)
	cfg.Enumeration.Color = sp(t.Sel)
	cfg.BlockQuote.Color = sp(t.Muted)
	cfg.HorizontalRule.Color = sp(t.Border)
	cfg.Code.Color = sp(t.Warn)
	cfg.Code.BackgroundColor = sp(t.Surface)
	cfg.CodeBlock.Color = sp(t.Text)
	cfg.CodeBlock.BackgroundColor = nil
	cfg.Table.Color = sp(t.Text)
	cfg.ImageText.Color = sp(t.Muted)
	cfg.Task.Ticked = "[" + GOK + "] "
	return cfg
}

// RenderMarkdown renders a README for the terminal in the current theme.
// dark is kept for callers; the theme decides light or dark.
func RenderMarkdown(md string, width int, dark bool) string {
	_ = dark
	r, err := glamour.NewTermRenderer(glamour.WithStyles(markdownStyle()), glamour.WithWordWrap(width))
	if err != nil {
		return md
	}
	s, err := r.Render(md)
	if err != nil {
		return md
	}
	return s
}
