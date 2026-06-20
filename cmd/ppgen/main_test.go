package main

import (
	"os"
	"path/filepath"
	"testing"

	"perfectpixel/internal/sprite"
)

// writeTempFrameCounts는 테스트용 frame_counts.json 미러를 임시 파일로 작성합니다.
func writeTempFrameCounts(t *testing.T) string {
	t.Helper()
	const body = `{
  "_comment": "테스트 미러",
  "apple":  { "idle": 4, "walk": 6, "attack": 4, "hit": 2, "death": 6, "spawn": 4, "victory": 4 },
  "banana": { "idle": 4, "walk": 6, "attack": 5, "hit": 2, "death": 6, "spawn": 4, "victory": 4 }
}`
	p := filepath.Join(t.TempDir(), "frame_counts.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("임시 JSON 작성 실패: %v", err)
	}
	return p
}

// presetsByName은 테스트 비교용으로 프리셋 슬라이스를 이름→Frames 맵으로 변환합니다.
func presetsByName(ps []sprite.PresetInfo) map[string]int {
	m := map[string]int{}
	for _, p := range ps {
		m[p.Name] = p.Frames
	}
	return m
}

func TestApplyFrameCounts_Override(t *testing.T) {
	fc := writeTempFrameCounts(t)
	// ppgen 프리셋 상태명(hurt) 포함 — frame_counts 의 game 상태명(hit)으로 매핑돼야 함.
	in, err := selectStates(options{states: "idle,walk,attack,hurt,death,victory"})
	if err != nil {
		t.Fatalf("selectStates 실패: %v", err)
	}

	out, err := applyFrameCounts(in, options{framecounts: fc, unit: "banana", quiet: true})
	if err != nil {
		t.Fatalf("applyFrameCounts 실패: %v", err)
	}
	got := presetsByName(out)

	// banana: attack=5, hurt(←hit)=2, death=6, walk=6, idle=4, victory=4
	want := map[string]int{"idle": 4, "walk": 6, "attack": 5, "hurt": 2, "death": 6, "victory": 4}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: Frames=%d, 기대=%d", name, got[name], w)
		}
	}
}

func TestApplyFrameCounts_NoFlagIsNoop(t *testing.T) {
	in, err := selectStates(options{states: "idle,attack"})
	if err != nil {
		t.Fatalf("selectStates 실패: %v", err)
	}
	out, err := applyFrameCounts(in, options{}) // framecounts 미지정
	if err != nil {
		t.Fatalf("no-op 인데 에러: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("no-op 인데 길이 변함: %d → %d", len(in), len(out))
	}
	// attack 은 presets.go 기본값(4) 유지.
	if presetsByName(out)["attack"] != 4 {
		t.Errorf("attack 기본 Frames=4 기대, got=%d", presetsByName(out)["attack"])
	}
}

func TestApplyFrameCounts_MissingUnitErrors(t *testing.T) {
	fc := writeTempFrameCounts(t)
	in, _ := selectStates(options{states: "idle"})
	if _, err := applyFrameCounts(in, options{framecounts: fc, unit: ""}); err == nil {
		t.Error("-unit 누락 시 에러를 기대했으나 nil")
	}
	if _, err := applyFrameCounts(in, options{framecounts: fc, unit: "dragonfruit"}); err == nil {
		t.Error("JSON 에 없는 -unit 에 에러를 기대했으나 nil")
	}
}
