package parser

import (
	"archive/zip"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"backend/internal/model"
)

func parsePptx(path, mediaDir string) (ParseResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer reader.Close()

	rawSlides := map[string]string{}
	rawNotes := map[string]string{}
	rawRels := map[string]string{}

	for _, file := range reader.File {
		isSlide := strings.HasPrefix(file.Name, "ppt/slides/slide") &&
			strings.HasSuffix(file.Name, ".xml")
		isNote := strings.HasPrefix(file.Name, "ppt/notesSlides/notesSlide") &&
			strings.HasSuffix(file.Name, ".xml")
		isRels := strings.HasPrefix(file.Name, "ppt/slides/_rels/slide") &&
			strings.HasSuffix(file.Name, ".rels")
		if !isSlide && !isNote && !isRels {
			continue
		}
		content, readErr := readZipFile(file)
		if readErr != nil {
			if isSlide || isNote {
				return ParseResult{}, readErr
			}
			continue
		}
		switch {
		case isSlide:
			rawSlides[file.Name] = string(content)
		case isNote:
			rawNotes[file.Name] = string(content)
		case isRels:
			rawRels[file.Name] = string(content)
		}
	}

	if len(rawSlides) == 0 {
		return ParseResult{}, errors.New("pptx content is empty")
	}

	slideRels := map[string]map[string]string{}
	slideImageRels := map[string]map[string]string{}
	for relsName, content := range rawRels {
		slideName := slideNameFromRels(relsName)
		slideRels[slideName] = parseOOXMLRels(content)
		slideImageRels[slideName] = parseOOXMLImageRels(content, "ppt/slides")
	}

	slideText := map[string]string{}
	slideRefs := map[string][]model.ResourceRef{}
	for slideName, content := range rawSlides {
		slideText[slideName] = extractOOXMLTextWithMarkdownTables(
			content,
			"a:p",
			"a:t",
			"a:tbl",
			"a:tr",
			"a:tc",
			false,
		)
		slideRefs[slideName] = extractPptxSlideRefs(
			content,
			slideRels[slideName],
			slideImageRels[slideName],
		)
	}

	noteText := map[string]string{}
	for noteName, content := range rawNotes {
		noteText[noteName] = extractParagraphText(content, "a:p", "a:t")
	}

	slideNames := make([]string, 0, len(slideText))
	for name := range slideText {
		slideNames = append(slideNames, name)
	}
	sort.Strings(slideNames)

	var blocks []ParseBlock
	for idx, name := range slideNames {
		section := strings.TrimSpace(slideText[name])
		refs := slideRefs[name]
		// append image placeholders so the text reflects image presence
		for _, ref := range refs {
			if ref.RefType == "image" {
				placeholder := "[Image]"
				if ref.Label != "" {
					placeholder = "[Image: " + ref.Label + "]"
				}
				section = strings.TrimSpace(section + "\n" + placeholder)
			}
		}
		noteName := filepath.Join(
			"ppt",
			"notesSlides",
			"notesSlide"+slideNumberFromName(name)+".xml",
		)
		note := strings.TrimSpace(noteText[noteName])
		if note != "" {
			section = strings.TrimSpace(section + "\nNotes: " + note)
		}
		if section != "" {
			slideNum := idx + 1
			label := "Slide " + strconv.Itoa(slideNum)
			blocks = append(blocks, ParseBlock{
				Text:         label + "\n" + section,
				SectionTitle: label,
				PageStart:    slideNum,
				Refs:         refs,
			})
		}
	}

	if len(blocks) == 0 {
		return ParseResult{}, errors.New("pptx content is empty")
	}

	if mediaDir != "" {
		resolveZIPMedia(&reader.Reader, blocks, mediaDir)
	}

	slideTexts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		slideTexts = append(slideTexts, b.Text)
	}
	text := strings.TrimSpace(strings.Join(slideTexts, "\n\n"))
	return ParseResult{Text: text, PageCount: len(blocks), Blocks: blocks}, nil
}
