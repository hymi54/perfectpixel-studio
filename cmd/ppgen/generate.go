package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"perfectpixel/internal/gen"
	"perfectpixel/internal/sprite"
)

// stateResult는 한 상태 생성의 결과 + 품질 측정값입니다.
type stateResult struct {
	Name     string
	Expected int
	Found    int
	Attempts int
	Score    int
	Identity float64
	Motion   float64
	Errors   []string
	frames   []*image.NRGBA
	rawClean *image.NRGBA
}

func (r stateResult) ok() bool { return r.Found == r.Expected && len(r.Errors) == 0 }

func (r stateResult) status() string {
	if r.ok() {
		return "ok"
	}
	if r.Found != r.Expected {
		return "frame-mismatch"
	}
	if len(r.Errors) > 0 {
		return "quality-issue"
	}
	return "partial"
}

func (r stateResult) row() resultRow {
	return resultRow{
		Name: r.Name, Expected: r.Expected, Found: r.Found, Attempts: r.Attempts,
		Score: r.Score, Identity: r.Identity, Motion: r.Motion,
		Status: r.status(), Errors: r.Errors,
	}
}

func decodeImg(raw []byte) (*image.NRGBA, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return sprite.ToNRGBA(img), nil
}

func pngBytes(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func savePNG(path string, img image.Image) {
	if b := pngBytes(img); len(b) > 0 {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// generateBase는 베이스 캐릭터를 생성하고 배경 제거 + 픽셀화한 정리본과 PNG 바이트를 반환합니다.
func generateBase(ctx context.Context, p gen.Provider, desc, styleKey, style string) (*image.NRGBA, []byte, error) {
	raw, err := p.GenerateImage(ctx, sprite.BuildCharacterPrompt(desc, styleKey, style), nil, "1:1")
	if err != nil {
		return nil, nil, err
	}
	img, err := decodeImg(raw)
	if err != nil {
		return nil, nil, err
	}
	clean := sprite.RemoveBackground(img)
	if palette := sprite.PaletteSizeForStyle(styleKey); palette > 0 {
		single := []*image.NRGBA{clean}
		sprite.PixelPostProcess(single, palette)
		clean = single[0]
	}
	return clean, pngBytes(clean), nil
}

// runExtractOnly는 생성 없이 raw 스트립 PNG 를 그대로 분할/추출합니다.
// GUI 의 rawStrip(추출 전, 배경 제거된 스트립)을 다시 추출해 분할 거동을 진단하거나,
// 마음에 드는 raw 를 제대로 재추출하는 용도. -states 의 프레임 수로 분할한다.
func runExtractOnly(opt options) error {
	logf := func(format string, a ...any) {
		if !opt.quiet && !opt.jsonOut {
			fmt.Printf(format, a...)
		}
	}
	presets, err := selectStates(opt)
	if err != nil {
		return err
	}
	presets, err = applyFrameCounts(presets, opt)
	if err != nil {
		return err
	}
	if len(presets) != 1 {
		return fmt.Errorf("-extractfile 은 한 번에 한 상태만 지원합니다 (-states 에 1개 지정). 현재 %d개", len(presets))
	}
	raw, err := os.ReadFile(opt.extractfile)
	if err != nil {
		return fmt.Errorf("-extractfile 읽기 실패: %w", err)
	}
	img, err := decodeImg(raw)
	if err != nil {
		return err
	}
	// rawStrip(GUI 의 추출 전 스트립)은 이미 배경 제거(투명)된 상태다. GUI 파이프라인과
	// 동일하게 RemoveBackground 를 추가로 적용하지 않고 그대로 추출한다 (이중 적용 시 투명
	// 배경을 오인해 캐릭터가 지워짐).
	clean := sprite.ToNRGBA(img)

	if err := os.MkdirAll(opt.out, 0o755); err != nil {
		return err
	}
	if opt.dumpraw {
		rawDir := filepath.Join(opt.out, "raw")
		_ = os.MkdirAll(rawDir, 0o755)
		savePNG(filepath.Join(rawDir, presets[0].Name+".png"), clean)
	}

	kw := presets[0]
	ext := sprite.ExtractFrames(clean, kw.Frames, 256, 256, 24)
	dir := filepath.Join(opt.out, "frames", kw.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, f := range ext.Frames {
		savePNG(filepath.Join(dir, fmt.Sprintf("frame-%02d.png", i)), f)
	}
	logf("[extractonly] %s: %d/%d 프레임 추출 → %s\n", kw.Name, ext.Found, kw.Frames, dir)
	for _, w := range ext.Warnings {
		logf("  경고: %s\n", w)
	}
	return nil
}

// loadBaseImage는 파일에서 베이스 이미지를 읽어 (정체성용 clean, ref용 png bytes)를 반환합니다.
// GUI 의 "이미지 업로드" 경로를 헤드리스에서 재현하기 위한 것(생성 대신 로드).
// 이미 투명 배경이면 RemoveBackground 는 무해하고, 배경이 있으면 키잉으로 제거한다.
func loadBaseImage(path string) (*image.NRGBA, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	img, err := decodeImg(raw)
	if err != nil {
		return nil, nil, err
	}
	clean := sprite.RemoveBackground(img)
	return clean, pngBytes(clean), nil
}

// genState는 앱과 동일한 자동 재시도 품질 보정 루프로 한 상태를 생성합니다.
func genState(ctx context.Context, p gen.Provider, opt options, style string,
	spec sprite.StateSpec, refs [][]byte, baseN *image.NRGBA) stateResult {

	expected := spec.Frames
	aspect := sprite.AspectForFrames(expected)
	palette := sprite.PaletteSizeForStyle(opt.style)
	feedback := ""
	maxAttempts := opt.attempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var best stateResult
	var bestInsp sprite.InspectResult
	bestScore := -1 << 30
	best.Name, best.Expected = spec.Name, expected

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		prompt := sprite.BuildStripPrompt(opt.desc, opt.style, style, spec, feedback)
		if len(refs) > 1 {
			prompt += "\nMotion reference: the second attached image is the FRONT-view animation strip of this same character performing this exact action. Reproduce the same motion timing and pose phases frame by frame, but viewed from the required facing direction above.\n"
		}
		raw, err := p.GenerateImage(ctx, prompt, refs, aspect)
		if err != nil {
			best.Errors = append(best.Errors, err.Error())
			return best
		}
		nimg, err := decodeImg(raw)
		if err != nil {
			continue
		}
		bgKey := sprite.DetectBackground(nimg)
		clean := sprite.RemoveBackground(nimg)
		ext := sprite.ExtractFrames(clean, expected, 256, 256, 24)
		insp := sprite.InspectFrames(ext.Frames, bgKey, baseN)
		// 픽셀화는 palette>0(pixel/retro16)일 때만. cartoon/chibi는 palette=0 → 매끈 유지.
		// (generateBase 와 동일한 가드로 의도를 명시 — PixelPostProcess 내부도 0이면 no-op)
		if palette > 0 {
			sprite.PixelPostProcess(ext.Frames, palette)
		}

		cand := stateResult{
			Name: spec.Name, Expected: expected, Found: ext.Found, Attempts: attempt,
			Motion: sprite.MotionPresence(ext.Frames), frames: ext.Frames, rawClean: clean,
		}
		cand.Errors = append(cand.Errors, insp.Errors...)

		if cand.ok() {
			qm := sprite.ScoreFrames(ext.Frames, expected, insp, cand.Motion)
			cand.Score, cand.Identity = qm.Score, qm.IdentityHash
			return cand
		}
		score := cand.Found*100 - len(cand.Errors)*10
		if score > bestScore {
			best, bestScore, bestInsp = cand, score, insp
		}

		var fixes []string
		if cand.Found != expected {
			fixes = append(fixes, fmt.Sprintf(
				"IMPORTANT CORRECTION: the last attempt read as %d poses but EXACTLY %d are required. Redraw as %d equal columns, one clearly separated pose per column, each ringed by a clean magenta gutter.",
				cand.Found, expected, expected))
		}
		fixes = append(fixes, insp.RetryHints...)
		feedback = strings.Join(fixes, "\n")
	}
	if len(best.frames) > 0 {
		qm := sprite.ScoreFrames(best.frames, expected, bestInsp, best.Motion)
		best.Score, best.Identity = qm.Score, qm.IdentityHash
	}
	return best
}
