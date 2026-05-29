package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"backend/internal/infra"
	"backend/internal/model"
	"go.uber.org/zap"
)

func parsePDF(path, mediaDir string) (ParseResult, error) {
	pythonBin := strings.TrimSpace(os.Getenv("PDF_PARSER_PYTHON"))
	if pythonBin == "" {
		pythonBin = "python3"
	}

	scriptPath := strings.TrimSpace(os.Getenv("PDF_PARSER_SCRIPT"))
	if scriptPath == "" {
		var err error
		scriptPath, err = pdfParserScriptPath()
		if err != nil {
			return ParseResult{}, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	args := []string{scriptPath, path}
	if mediaDir != "" {
		args = append(args, mediaDir)
	}
	infra.L.Info("running pdf parser",
		zap.String("python", pythonBin),
		zap.String("script", scriptPath),
		zap.String("file", path),
		zap.String("media_dir", mediaDir),
		zap.Strings("argv", append([]string{pythonBin}, args...)),
	)

	cmd := exec.CommandContext(ctx, pythonBin, args...)
	if mediaDir != "" {
		cmd.Env = append(os.Environ(), "PDF_PARSER_STORAGE_BASE="+staticResourceBase(mediaDir))
	}
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if msg == "" {
				msg = err.Error()
			}
			return ParseResult{}, fmt.Errorf(
				"pdf parser failed (python=%s script=%s file=%s): %s",
				pythonBin,
				scriptPath,
				path,
				msg,
			)
		}
		if ctx.Err() == context.DeadlineExceeded {
			return ParseResult{}, fmt.Errorf(
				"pdf parser timed out (python=%s script=%s file=%s)",
				pythonBin,
				scriptPath,
				path,
			)
		}
		return ParseResult{}, fmt.Errorf(
			"pdf parser exec failed (python=%s script=%s file=%s): %w",
			pythonBin,
			scriptPath,
			path,
			err,
		)
	}

	var payload struct {
		Text      string `json:"text"`
		PageCount int    `json:"page_count"`
		Pages     []struct {
			Text string              `json:"text"`
			Page int                 `json:"page"`
			Refs []model.ResourceRef `json:"refs"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return ParseResult{}, fmt.Errorf(
			"pdf parser returned invalid json (script=%s file=%s): %w",
			scriptPath,
			path,
			err,
		)
	}
	payload.Text = strings.TrimSpace(payload.Text)
	if payload.Text == "" {
		return ParseResult{}, errors.New("pdf content is empty")
	}
	if payload.PageCount < 1 {
		payload.PageCount = 1
	}
	blocks := make([]ParseBlock, 0, len(payload.Pages))
	for _, p := range payload.Pages {
		t := strings.TrimSpace(p.Text)
		if t == "" && len(p.Refs) == 0 {
			continue
		}
		refs := p.Refs
		if refs == nil {
			refs = []model.ResourceRef{}
		}
		blocks = append(blocks, ParseBlock{Text: t, PageStart: p.Page, PageEnd: p.Page, Refs: refs})
	}
	return ParseResult{Text: payload.Text, PageCount: payload.PageCount, Blocks: blocks}, nil
}

func pdfParserScriptPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve pdf parser script path: runtime caller unavailable")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "scripts", "parse_pdf.py"))
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
