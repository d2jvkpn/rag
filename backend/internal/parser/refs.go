package parser

import (
	"path"
	"regexp"
	"strings"

	"backend/internal/model"
	"github.com/google/uuid"
)

// --- ref extraction helpers ---

var (
	mdImageRefRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]*)\)`)
	mdLinkRefRe  = regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)

	relsElemRe   = regexp.MustCompile(`(?s)<Relationship\s[^<]*/?>`)
	relsIDRe     = regexp.MustCompile(`\bId="([^"]*)"`)
	relsTargetRe = regexp.MustCompile(`\bTarget="([^"]*)"`)
	relsTypeRe   = regexp.MustCompile(`\bType="([^"]*)"`)

	docxHyperlinkRe      = regexp.MustCompile(`(?s)<w:hyperlink\s([^>]*)>(.*?)</w:hyperlink>`)
	docxHyperlinkRidAttr = regexp.MustCompile(`r:id="([^"]*)"`)
	docxRunTextRe        = regexp.MustCompile(`(?s)<w:t[^>]*>([^<]*)</w:t>`)
	docxDrawingBlockRe   = regexp.MustCompile(`(?s)<w:drawing[\s>].*?</w:drawing>`)
	docxDocPrDescrRe     = regexp.MustCompile(`<wp:docPr\b[^>]*\bdescr="([^"]*)"`)
	docxDocPrTitleRe     = regexp.MustCompile(`<wp:docPr\b[^>]*\btitle="([^"]*)"`)

	pptxRunRe        = regexp.MustCompile(`(?s)<a:r\b[^>]*>(.*?)</a:r>`)
	pptxHlinkClickRe = regexp.MustCompile(`<a:hlinkClick\b[^>]*r:id="([^"]*)"`)
	pptxRunTextRe    = regexp.MustCompile(`(?s)<a:t[^>]*>([^<]*)</a:t>`)
	pptxPicRe        = regexp.MustCompile(`(?s)<p:pic\b.*?</p:pic>`)
	pptxCNvPrDescrRe = regexp.MustCompile(`<p:cNvPr\b[^>]*\bdescr="([^"]*)"`)
	pptxCNvPrNameRe  = regexp.MustCompile(`<p:cNvPr\b[^>]*\bname="([^"]*)"`)

	// shared between DOCX and PPTX: extracts r:embed from <a:blip>
	blipEmbedRe = regexp.MustCompile(`<a:blip\b[^>]*\br:embed="([^"]*)"`)
)

func isExternalURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// parseOOXMLRels parses an OOXML .rels file and returns a map of rId → target URL
// for hyperlink relationships only.
func parseOOXMLRels(content string) map[string]string {
	rels := map[string]string{}
	for _, elem := range relsElemRe.FindAllString(content, -1) {
		idM := relsIDRe.FindStringSubmatch(elem)
		targetM := relsTargetRe.FindStringSubmatch(elem)
		typeM := relsTypeRe.FindStringSubmatch(elem)
		if idM == nil || targetM == nil || typeM == nil {
			continue
		}
		if strings.Contains(typeM[1], "/hyperlink") {
			rels[idM[1]] = targetM[1]
		}
	}
	return rels
}

// parseOOXMLImageRels parses an OOXML .rels file and returns a map of
// rId → ZIP-internal file path for image relationships. baseDir is the
// directory containing the rels file's parent (e.g. "word" for DOCX,
// "ppt/slides" for PPTX) used to resolve relative targets.
func parseOOXMLImageRels(content, baseDir string) map[string]string {
	rels := map[string]string{}
	for _, elem := range relsElemRe.FindAllString(content, -1) {
		idM := relsIDRe.FindStringSubmatch(elem)
		targetM := relsTargetRe.FindStringSubmatch(elem)
		typeM := relsTypeRe.FindStringSubmatch(elem)
		if idM == nil || targetM == nil || typeM == nil {
			continue
		}
		if strings.Contains(typeM[1], "/image") {
			zipPath := path.Join(baseDir, targetM[1])
			rels[idM[1]] = zipPath
		}
	}
	return rels
}

// resolveZIPMedia walks all image refs in blocks whose StoragePath holds a
// temporary ZIP-internal path, extracts the file from the ZIP, writes it to

func extractMarkdownRefs(text string) (string, []model.ResourceRef) {
	var refs []model.ResourceRef

	// images before links: image syntax is a superset of link syntax
	text = mdImageRefRe.ReplaceAllStringFunc(text, func(match string) string {
		m := mdImageRefRe.FindStringSubmatch(match)
		alt, url := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "image",
			Label:      alt,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
		if alt != "" {
			return "[Image: " + alt + "]"
		}
		return "[Image]"
	})

	// links: keep anchor text, strip URL
	text = mdLinkRefRe.ReplaceAllStringFunc(text, func(match string) string {
		m := mdLinkRefRe.FindStringSubmatch(match)
		anchor, url := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "link",
			AnchorText: anchor,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
		return anchor
	})

	return text, refs
}

// extractDocxParaRefs extracts hyperlink and image refs from a DOCX paragraph
// XML fragment. It also returns an image placeholder string when the paragraph
// contains a drawing but no text runs (so the caller can substitute it).
// imageRels maps rId to ZIP-internal path; image refs have StoragePath set to
// that path as a temporary value resolved later by resolveZIPMedia.
func extractDocxParaRefs(
	paraXML string,
	rels map[string]string,
	imageRels map[string]string,
) (imgPlaceholder string, refs []model.ResourceRef) {
	for _, m := range docxHyperlinkRe.FindAllStringSubmatch(paraXML, -1) {
		attrs, inner := m[1], m[2]
		url := ""
		if rm := docxHyperlinkRidAttr.FindStringSubmatch(attrs); rm != nil && rels != nil {
			url = rels[rm[1]]
		}
		var parts []string
		for _, tm := range docxRunTextRe.FindAllStringSubmatch(inner, -1) {
			parts = append(parts, tm[1])
		}
		anchor := strings.TrimSpace(strings.Join(parts, ""))
		if anchor == "" && url == "" {
			continue
		}
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "link",
			AnchorText: anchor,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
	}

	var placeholders []string
	for _, drawXML := range docxDrawingBlockRe.FindAllString(paraXML, -1) {
		label := ""
		if m := docxDocPrDescrRe.FindStringSubmatch(drawXML); m != nil {
			label = htmlUnescape(m[1])
		} else if m := docxDocPrTitleRe.FindStringSubmatch(drawXML); m != nil {
			label = htmlUnescape(m[1])
		}
		storePath := ""
		if imageRels != nil {
			if bm := blipEmbedRe.FindStringSubmatch(drawXML); bm != nil {
				storePath = imageRels[bm[1]]
			}
		}
		refs = append(refs, model.ResourceRef{
			RefID:       uuid.Must(uuid.NewV7()).String(),
			RefType:     "image",
			Label:       label,
			StoragePath: storePath,
		})
		if label != "" {
			placeholders = append(placeholders, "[Image: "+label+"]")
		} else {
			placeholders = append(placeholders, "[Image]")
		}
	}
	imgPlaceholder = strings.Join(placeholders, "\n")
	return imgPlaceholder, refs
}

// extractPptxSlideRefs extracts hyperlink and image refs from a PPTX slide XML.
// imageRels maps rId to ZIP-internal path; image refs have StoragePath set to
// that path as a temporary value resolved later by resolveZIPMedia.
func extractPptxSlideRefs(
	slideXML string,
	rels map[string]string,
	imageRels map[string]string,
) []model.ResourceRef {
	var refs []model.ResourceRef

	for _, m := range pptxRunRe.FindAllStringSubmatch(slideXML, -1) {
		inner := m[1]
		hm := pptxHlinkClickRe.FindStringSubmatch(inner)
		if hm == nil {
			continue
		}
		url := ""
		if rels != nil {
			url = rels[hm[1]]
		}
		var parts []string
		for _, tm := range pptxRunTextRe.FindAllStringSubmatch(inner, -1) {
			parts = append(parts, tm[1])
		}
		anchor := strings.TrimSpace(strings.Join(parts, ""))
		if anchor == "" && url == "" {
			continue
		}
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "link",
			AnchorText: anchor,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
	}

	for _, picXML := range pptxPicRe.FindAllString(slideXML, -1) {
		label := ""
		if m := pptxCNvPrDescrRe.FindStringSubmatch(picXML); m != nil {
			label = htmlUnescape(m[1])
		} else if m := pptxCNvPrNameRe.FindStringSubmatch(picXML); m != nil {
			label = htmlUnescape(m[1])
		}
		storePath := ""
		if imageRels != nil {
			if bm := blipEmbedRe.FindStringSubmatch(picXML); bm != nil {
				storePath = imageRels[bm[1]]
			}
		}
		refs = append(refs, model.ResourceRef{
			RefID:       uuid.Must(uuid.NewV7()).String(),
			RefType:     "image",
			Label:       label,
			StoragePath: storePath,
		})
	}

	return refs
}

// slideNameFromRels derives the slide file path from its rels file path.
// e.g. "ppt/slides/_rels/slide1.xml.rels" → "ppt/slides/slide1.xml"
func slideNameFromRels(relsPath string) string {
	s := strings.Replace(relsPath, "/_rels/", "/", 1)
	return strings.TrimSuffix(s, ".rels")
}
