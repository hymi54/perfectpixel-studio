package sprite

import (
	"image"
	"testing"
)

// TestExtractAssignsPropToNearestBody는 한 포즈의 장비(칼)가 옆으로 길게 뻗어 인접
// 포즈의 컬럼 범위를 침범해도, 그 칼이 무게중심상 가까운 본체(자기 포즈)에 귀속되어
// 인접 프레임에 새어 들어가지 않는지 검증합니다 (GUI idle 칼 불일치의 근본 케이스).
func TestExtractAssignsPropToNearestBody(t *testing.T) {
	strip := image.NewNRGBA(image.Rect(0, 0, 400, 100))
	// 포즈A(왼): 사과(빨강) + 칼A(회색, A 왼쪽)
	fillBox(strip, 70, 30, 130, 90, 200, 60, 60)
	fillBox(strip, 40, 55, 70, 62, 180, 180, 180)
	// 포즈B(오른): 사과(빨강) + 칼B(파랑) — 칼B가 왼쪽으로 길게 뻗어 A-B 경계를 넘어
	// A세그 컬럼을 침범한다. 무게중심은 B 쪽이라 B에 귀속돼야 한다.
	fillBox(strip, 270, 30, 330, 90, 200, 60, 60)
	fillBox(strip, 170, 55, 260, 62, 60, 60, 200)

	res := ExtractFrames(strip, 2, 200, 200, 8)
	if len(res.Frames) != 2 {
		t.Fatalf("프레임 수 오류: %d (기대 2)", len(res.Frames))
	}
	// frame0(포즈A)에 칼B(파랑)가 침범하면 안 됨.
	if blue := countStrayPixels(res.Frames[0]); blue > 0 {
		t.Fatalf("포즈A 프레임에 옆 포즈 칼(파랑) 침범: %d px (기대 0)", blue)
	}
	// frame1(포즈B)에는 자기 칼(파랑)이 있어야 함.
	if blue := countStrayPixels(res.Frames[1]); blue == 0 {
		t.Fatalf("포즈B 프레임에 자기 칼(파랑)이 사라짐 (기대 >0)")
	}
}

// countStrayPixels는 프레임에서 "stray(파랑) 조각" 픽셀 수를 셉니다.
// 본체는 빨강(r=200,b=60), stray 는 파랑(r=60,b=200) 으로 칠해 색으로 구분합니다.
func countStrayPixels(f *image.NRGBA) int {
	n := 0
	for i := 0; i+3 < len(f.Pix); i += 4 {
		if f.Pix[i+3] <= alphaThreshold {
			continue
		}
		r, b := f.Pix[i], f.Pix[i+2]
		if b > 150 && r < 120 { // 파랑 우세 = stray
			n++
		}
	}
	return n
}

func countBodyPixels(f *image.NRGBA) int {
	n := 0
	for i := 0; i+3 < len(f.Pix); i += 4 {
		if f.Pix[i+3] <= alphaThreshold {
			continue
		}
		r, b := f.Pix[i], f.Pix[i+2]
		if r > 150 && b < 120 { // 빨강 우세 = 본체
			n++
		}
	}
	return n
}

// TestExtractExcludesDetachedStray는 한 포즈의 컬럼 범위 안에 세로로 분리된
// stray 조각(예: 옆 포즈에서 넘어온 칼끝)이 있을 때, 추출 프레임이 그 조각을
// 포함하지 않고 본체만 담아야 함을 검증합니다.
//
// 현재 extractContent 는 컬럼 범위의 알파 픽셀을 2D 연결성 무시하고 전부 복사하므로
// 이 테스트는 RED(실패)여야 정상입니다.
func TestExtractExcludesDetachedStray(t *testing.T) {
	strip := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	// 본체(빨강): 아래쪽 큰 덩어리
	fillBox(strip, 60, 40, 140, 95, 200, 60, 60)
	// stray(파랑): 본체와 세로로 떨어졌지만 본체의 x-범위 안 → 1D 투영으론 같은 런
	fillBox(strip, 85, 5, 105, 22, 60, 60, 200)

	res := ExtractFrames(strip, 1, 200, 200, 8)
	if len(res.Frames) != 1 {
		t.Fatalf("프레임 수 오류: %d (기대 1)", len(res.Frames))
	}
	f := res.Frames[0]

	if body := countBodyPixels(f); body == 0 {
		t.Fatalf("본체 픽셀이 사라짐 (기대 >0)")
	}
	if stray := countStrayPixels(f); stray != 0 {
		t.Fatalf("분리된 stray 조각이 프레임에 포함됨: %d 픽셀 (기대 0)", stray)
	}
}

// TestExtractKeepsDetachedLargeAttachment는 본체와 떨어져 있어도 면적이 큰 부착물
// (예: 손에서 살짝 분리돼 그려진 방패)은 보존되어야 함을 검증합니다 — 과도한 조각
// 제거(가장 큰 컴포넌트만 남기는 식)로 정당한 장비를 잃지 않도록 하는 가드.
func TestExtractKeepsDetachedLargeAttachment(t *testing.T) {
	strip := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	// 본체(빨강): 위쪽 큰 덩어리
	fillBox(strip, 70, 20, 140, 70, 200, 60, 60)
	// 방패(파랑): 본체와 세로로 떨어졌지만 x-범위가 겹쳐 같은 컬럼 런(1포즈)이고,
	// 면적이 본체의 keepFrac 이상 → 보존돼야 함
	fillBox(strip, 55, 78, 100, 95, 60, 60, 200)

	res := ExtractFrames(strip, 1, 200, 200, 8)
	if len(res.Frames) != 1 {
		t.Fatalf("프레임 수 오류: %d (기대 1)", len(res.Frames))
	}
	f := res.Frames[0]
	if body := countBodyPixels(f); body == 0 {
		t.Fatalf("본체 픽셀이 사라짐 (기대 >0)")
	}
	if att := countStrayPixels(f); att == 0 {
		t.Fatalf("큰 부착물(방패)이 잘못 제거됨 (기대 >0)")
	}
}
