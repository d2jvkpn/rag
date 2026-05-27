package parser

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
)

func resolveZIPMedia(reader *zip.Reader, blocks []ParseBlock, mediaDir string) {
	mediaDir = filepath.Clean(mediaDir)
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return
	}
	storageBase := staticResourceBase(mediaDir)
	seen := map[string]string{} // zipPath → relative storage path (or "" on failure)
	for bi := range blocks {
		for ri := range blocks[bi].Refs {
			ref := &blocks[bi].Refs[ri]
			if ref.RefType != "image" || ref.StoragePath == "" {
				continue
			}
			zipPath := ref.StoragePath
			if relPath, ok := seen[zipPath]; ok {
				ref.StoragePath = relPath
				continue
			}
			data := readFromZIP(reader, zipPath)
			if len(data) == 0 {
				ref.StoragePath = ""
				seen[zipPath] = ""
				continue
			}
			diskPath := filepath.Join(mediaDir, filepath.Base(zipPath))
			if err := os.WriteFile(diskPath, data, 0o644); err != nil {
				ref.StoragePath = ""
				seen[zipPath] = ""
				continue
			}
			relPath, err := filepath.Rel(storageBase, diskPath)
			if err != nil {
				relPath = diskPath
			}
			ref.StoragePath = relPath
			seen[zipPath] = relPath
		}
	}
}

func staticResourceBase(mediaDir string) string {
	mediaDir = filepath.Clean(mediaDir)
	leaf := filepath.Base(mediaDir)
	if isDatedDocumentDirName(leaf) {
		return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(mediaDir))))
	}
	return filepath.Dir(mediaDir)
}

func isDatedDocumentDirName(name string) bool {
	if len(name) < len("2006-01-02_") || name[4] != '-' || name[7] != '-' || name[10] != '_' {
		return false
	}
	for _, idx := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		if name[idx] < '0' || name[idx] > '9' {
			return false
		}
	}
	return true
}

func readFromZIP(reader *zip.Reader, name string) []byte {
	for _, f := range reader.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			data, _ := io.ReadAll(rc)
			return data
		}
	}
	return nil
}
