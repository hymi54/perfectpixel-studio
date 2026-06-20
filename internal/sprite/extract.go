package sprite

import (
	"fmt"
	"image"
	"math"

	xdraw "golang.org/x/image/draw"
)

const alphaThreshold = 10 // 이 값 이하의 알파는 빈(투명) 픽셀로 취급

// frameContent는 스트립 좌표계에서 추출한 한 포즈의 콘텐츠입니다.
type frameContent struct {
	img    *image.NRGBA // bbox로 자른 콘텐츠
	minX   int
	cx     float64 // 알파 가중 질량 중심 X (스트립 좌표)
	bottom int     // 베이스라인(콘텐츠 최하단 행, 스트립 좌표)
}

// component는 한 연결요소의 질량·경계상자·무게중심 x 입니다.
type component struct {
	mass                   float64
	minX, minY, maxX, maxY int
	cx                     float64 // 알파 가중 무게중심 x (스트립 좌표)
}

// labelStripComponents는 전체 스트립을 8-연결 연결요소로 라벨링해, label 배열
// (w*h, 0=빈, >0=컴포넌트 번호) 과 컴포넌트 목록을 반환합니다.
//
// 포즈 분할(컬럼 단위)과 달리, 칼처럼 옆으로 길게 뻗어 인접 포즈 컬럼을 침범하는
// 장비를 하나의 컴포넌트로 온전히 잡아내, 무게중심 기준으로 올바른 포즈에 귀속할 수
// 있게 한다.
func labelStripComponents(strip *image.NRGBA) ([]int, []component) {
	w, h := strip.Rect.Dx(), strip.Rect.Dy()
	label := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if strip.Pix[strip.PixOffset(x, y)+3] > alphaThreshold {
				label[y*w+x] = -1
			}
		}
	}
	var comps []component
	stackX := make([]int, 0, 256)
	stackY := make([]int, 0, 256)
	for y0 := 0; y0 < h; y0++ {
		for x0 := 0; x0 < w; x0++ {
			if label[y0*w+x0] != -1 {
				continue
			}
			id := len(comps) + 1
			c := component{minX: w, minY: h, maxX: -1, maxY: -1}
			var sumWX, sumW float64
			stackX = stackX[:0]
			stackY = stackY[:0]
			label[y0*w+x0] = id
			stackX = append(stackX, x0)
			stackY = append(stackY, y0)
			for len(stackX) > 0 {
				cx := stackX[len(stackX)-1]
				cy := stackY[len(stackY)-1]
				stackX = stackX[:len(stackX)-1]
				stackY = stackY[:len(stackY)-1]
				a := float64(strip.Pix[strip.PixOffset(cx, cy)+3])
				c.mass += a
				sumWX += float64(cx) * a
				sumW += a
				if cx < c.minX {
					c.minX = cx
				}
				if cx > c.maxX {
					c.maxX = cx
				}
				if cy < c.minY {
					c.minY = cy
				}
				if cy > c.maxY {
					c.maxY = cy
				}
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						if dx == 0 && dy == 0 {
							continue
						}
						nx, ny := cx+dx, cy+dy
						if nx < 0 || nx >= w || ny < 0 || ny >= h {
							continue
						}
						if label[ny*w+nx] == -1 {
							label[ny*w+nx] = id
							stackX = append(stackX, nx)
							stackY = append(stackY, ny)
						}
					}
				}
			}
			if sumW > 0 {
				c.cx = sumWX / sumW
			} else {
				c.cx = float64(c.minX+c.maxX) / 2
			}
			comps = append(comps, c)
		}
	}
	return label, comps
}

// compModes는 각 컴포넌트를 두 모드로 분류합니다:
//   - colMode=true  : bbox가 2개 이상의 포즈 중심을 가로로 포함 → "닿아 한 덩어리가 된
//     본체"로 보고, 프레임 추출 시 컬럼 경계로 나눈다(닿은 포즈 분리).
//   - colMode=false : 그 외(칼·방패 등 단일 포즈 장비) → 무게중심이 가장 가까운 세그에
//     통째로 귀속한다(옆으로 뻗어 인접 컬럼을 침범해도 자기 포즈로).
//
// segOf 는 colMode=false 컴포넌트의 귀속 세그 인덱스입니다.
func compModes(comps []component, centers []float64) (colMode []bool, segOf []int) {
	colMode = make([]bool, len(comps))
	segOf = make([]int, len(comps))
	for ci := range comps {
		c := comps[ci]
		cnt := 0
		for _, ctr := range centers {
			if float64(c.minX) <= ctr && ctr <= float64(c.maxX) {
				cnt++
			}
		}
		if cnt >= 2 {
			colMode[ci] = true
			continue
		}
		best, bd := 0, math.Inf(1)
		for si, ctr := range centers {
			if d := math.Abs(c.cx - ctr); d < bd {
				bd, best = d, si
			}
		}
		segOf[ci] = best
	}
	return colMode, segOf
}

// buildFrameForSeg는 세그 si 의 frameContent 를 만듭니다.
//
//   - colMode 컴포넌트(닿은 본체): 이 세그의 컬럼 범위 [seg.start,seg.end) 픽셀만 포함.
//   - 무게중심이 이 세그에 귀속된 컴포넌트: 본체(최대 질량) + 질량 keepFrac 이상 또는
//     본체에 인접한 것만 보존(작은 외래 조각 배제). 다른 세그 귀속 칼은 자동 제외.
func buildFrameForSeg(strip *image.NRGBA, label []int, comps []component, colMode []bool, segOf []int, seg colSpan, si, w, h int) frameContent {
	// 이 세그에 무게중심 귀속된 컴포넌트들
	var segComps []int
	for ci := range comps {
		if !colMode[ci] && segOf[ci] == si {
			segComps = append(segComps, ci)
		}
	}
	// 본체 질량 후보: 귀속 컴포넌트 + 이 세그에 걸친 colMode 컴포넌트
	domMass := 0.0
	for _, ci := range segComps {
		if comps[ci].mass > domMass {
			domMass = comps[ci].mass
		}
	}
	for ci := range comps {
		if colMode[ci] && comps[ci].minX < seg.end && comps[ci].maxX >= seg.start {
			if comps[ci].mass > domMass {
				domMass = comps[ci].mass
			}
		}
	}
	// 귀속 컴포넌트 보존 규칙(keepFrac/인접)으로 작은 외래 조각 배제
	const keepFrac = 0.15
	gapTol := h / 60
	if gapTol < 3 {
		gapTol = 3
	}
	keepC := make(map[int]bool, len(segComps))
	for _, ci := range segComps {
		if comps[ci].mass >= keepFrac*domMass {
			keepC[ci] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, ci := range segComps {
			if keepC[ci] {
				continue
			}
			for kj := range keepC {
				if compGap(comps[ci], comps[kj]) <= gapTol {
					keepC[ci] = true
					changed = true
					break
				}
			}
		}
	}

	// 이 세그에 포함될 픽셀 판정
	include := func(x, y int) bool {
		l := label[y*w+x]
		if l <= 0 {
			return false
		}
		ci := l - 1
		if colMode[ci] {
			return x >= seg.start && x < seg.end
		}
		return segOf[ci] == si && keepC[ci]
	}

	minX, minY, maxX, maxY := w, h, -1, -1
	var sumWX, sumW float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !include(x, y) {
				continue
			}
			a := strip.Pix[strip.PixOffset(x, y)+3]
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
			sumWX += float64(x) * float64(a)
			sumW += float64(a)
		}
	}
	if maxX < minX || maxY < minY {
		return frameContent{}
	}
	gw, gh := maxX-minX+1, maxY-minY+1
	dst := image.NewNRGBA(image.Rect(0, 0, gw, gh))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if !include(x, y) {
				continue
			}
			si := strip.PixOffset(x, y)
			di := dst.PixOffset(x-minX, y-minY)
			copy(dst.Pix[di:di+4], strip.Pix[si:si+4])
		}
	}
	cx := float64(minX+maxX+1) / 2
	if sumW > 0 {
		cx = sumWX / sumW
	}
	return frameContent{img: dst, minX: minX, cx: cx, bottom: maxY}
}

// compGap은 두 컴포넌트 경계상자 사이의 체비셰프 간극(겹치면 0)을 반환합니다.
func compGap(a, b component) int {
	dx := 0
	if a.minX > b.maxX {
		dx = a.minX - b.maxX
	} else if b.minX > a.maxX {
		dx = b.minX - a.maxX
	}
	dy := 0
	if a.minY > b.maxY {
		dy = a.minY - b.maxY
	} else if b.minY > a.maxY {
		dy = b.minY - a.maxY
	}
	if dx > dy {
		return dx
	}
	return dy
}

// ExtractFrames는 투명 배경 스트립에서 포즈를 투영 분할로 검출해 셀 크기 프레임으로
// 만듭니다. 모든 프레임에 공통 스케일을 적용하고, 질량 중심으로 수평 정렬하며,
// 공통 베이스라인 기준으로 수직 오프셋(점프 호 등)을 보존합니다.
func ExtractFrames(strip *image.NRGBA, expected, cellW, cellH, margin int) ExtractResult {
	res := ExtractResult{Expected: expected}
	segs, natural := segmentStrip(strip, expected)
	if len(segs) == 0 {
		res.Warnings = append(res.Warnings, "이미지에서 캐릭터를 찾지 못했습니다. 다시 생성해 주세요.")
		return res
	}
	w, h := strip.Rect.Dx(), strip.Rect.Dy()

	// 전체 스트립을 1회 연결요소 라벨링하고, 각 컴포넌트를 모드로 분류한다(닿은 본체는
	// 컬럼 분할, 칼·장비는 무게중심 세그 귀속). 칼이 옆으로 뻗어 인접 컬럼을 침범해도
	// 무게중심이 자기 포즈 쪽이면 올바른 프레임으로 들어간다.
	label, comps := labelStripComponents(strip)
	centers := make([]float64, len(segs))
	for i, s := range segs {
		centers[i] = float64(s.start+s.end) / 2
	}
	colMode, segOf := compModes(comps, centers)

	var fcs []frameContent
	for si := range segs {
		fc := buildFrameForSeg(strip, label, comps, colMode, segOf, segs[si], si, w, h)
		if fc.img != nil {
			fcs = append(fcs, fc)
		}
	}
	if len(fcs) == 0 {
		res.Warnings = append(res.Warnings, "유효한 포즈를 찾지 못했습니다. 다시 생성해 주세요.")
		return res
	}

	// 공통 베이스라인 + 공유 스케일
	baseline := 0
	for _, g := range fcs {
		if g.bottom > baseline {
			baseline = g.bottom
		}
	}
	availW := cellW - margin*2
	availH := cellH - margin*2
	if availW < 8 || availH < 8 {
		availW, availH = cellW, cellH
	}
	maxW, maxEffH := 1, 1
	for _, g := range fcs {
		offset := baseline - g.bottom
		if g.img.Rect.Dx() > maxW {
			maxW = g.img.Rect.Dx()
		}
		if eff := g.img.Rect.Dy() + offset; eff > maxEffH {
			maxEffH = eff
		}
	}
	scale := minf(float64(availW)/float64(maxW), float64(availH)/float64(maxEffH))
	if scale > 1 {
		scale = 1
	}

	for _, g := range fcs {
		sw := int(float64(g.img.Rect.Dx())*scale + 0.5)
		sh := int(float64(g.img.Rect.Dy())*scale + 0.5)
		if sw < 1 {
			sw = 1
		}
		if sh < 1 {
			sh = 1
		}
		scaled := g.img
		if sw != g.img.Rect.Dx() || sh != g.img.Rect.Dy() {
			scaled = image.NewNRGBA(image.Rect(0, 0, sw, sh))
			xdraw.CatmullRom.Scale(scaled, scaled.Rect, g.img, g.img.Rect, xdraw.Over, nil)
		}
		offset := int(float64(baseline-g.bottom)*scale + 0.5)

		cell := image.NewNRGBA(image.Rect(0, 0, cellW, cellH))
		// 질량 중심이 셀 중앙에 오도록 수평 배치 (팔다리가 한쪽으로 뻗어도
		// 면적이 큰 몸통이 지배해 프레임 간 흔들림이 적음).
		left := int(float64(cellW)/2 - (g.cx-float64(g.minX))*scale + 0.5)
		if left < 0 {
			left = 0
		}
		if left+sw > cellW {
			left = cellW - sw
		}
		top := cellH - margin - offset - sh
		if top < 0 {
			top = 0
		}
		xdraw.Copy(cell, image.Point{X: left, Y: top}, scaled, scaled.Rect, xdraw.Over, nil)
		res.Frames = append(res.Frames, cell)
	}

	res.Found = natural
	if natural != expected {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("기대한 %d개와 다른 %d개의 포즈가 감지되었습니다. 포즈가 겹쳤거나 누락됐을 수 있어 재생성을 권장합니다.", expected, natural))
	}
	return res
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
