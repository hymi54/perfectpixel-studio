// Command ppgen은 GUI 없이 캐릭터 + 애니메이션 상태를 생성하고
// 게임 엔진에 바로 쓸 수 있는 번들(스프라이트시트 · manifest.json · Aseprite JSON ·
// 상태별 GIF/APNG · 개별 프레임 PNG)을 디스크로 내보내는 헤드리스 CLI입니다.
//
// 설치형 Wails 앱의 GenerateState + ExportProject 로직을 동일하게 재현하되,
// 파일 대화상자 없이 -out 디렉토리로 바로 내보내고, 결과 요약을 stdout에
// JSON으로 출력하여 스킬/스크립트에서 호출하기 좋게 만들었습니다.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/webp"

	"perfectpixel/internal/config"
	"perfectpixel/internal/gen"
	"perfectpixel/internal/sprite"
)

// options는 CLI 플래그를 묶은 실행 옵션입니다.
type options struct {
	desc     string
	style    string
	states   string
	percat   int
	all      bool
	dirset   string
	out      string
	provider string
	key      string
	model    string
	attempts int
	timeout  time.Duration
	jsonOut  bool
	quiet    bool
	baseOnly bool

	framecounts string // frame_counts.json 경로 (유닛별 프레임 수 오버라이드)
	unit        string // framecounts JSON 내 유닛 키 (apple/banana/…)
	dumpraw     bool   // 추출 전 clean 스트립을 <out>/raw/<state>.png 로 저장 (진단용)
	baseimg     string // 베이스 생성 대신 이 이미지 파일을 base 로 사용 (GUI 업로드 재현용)
	extractfile string // 생성 없이 이 raw 스트립 PNG 를 추출만 함 (분할 진단·재추출)
}

func main() {
	var (
		opt  options
		dump = flag.Bool("dump", false, "프리셋+방향 카탈로그를 JSON으로 출력하고 종료")
	)
	flag.StringVar(&opt.desc, "desc", "a small knight with silver armor and a blue plume on the helmet", "캐릭터 설명")
	flag.StringVar(&opt.style, "style", "cartoon", "스타일 키 (pixel | chibi | cartoon | retro16) — Fruit War 포크 기본값은 매끈한 cartoon (픽셀화 안 됨)")
	flag.StringVar(&opt.states, "states", "idle,walk", "쉼표로 구분된 생성할 상태 이름 목록")
	flag.IntVar(&opt.percat, "percat", 0, "0보다 크면 카테고리당 N개 프리셋을 자동 선택 (states 무시)")
	flag.BoolVar(&opt.all, "all", false, "전체 프리셋 생성 (states/percat 무시)")
	flag.StringVar(&opt.dirset, "dirset", "", "8방향 세트를 추가 생성할 상태 이름 (선택)")
	flag.StringVar(&opt.out, "out", "./perfectpixel-out", "출력 디렉토리")
	flag.StringVar(&opt.provider, "provider", "", "프로바이더 강제 지정 (gemini|openrouter|fal|byteplus)")
	flag.StringVar(&opt.key, "key", "", "API 키 강제 지정 (설정/환경변수보다 우선)")
	flag.StringVar(&opt.model, "model", "", "모델 강제 지정")
	flag.IntVar(&opt.attempts, "attempts", 3, "상태별 품질 보정 재생성 최대 시도 횟수")
	flag.DurationVar(&opt.timeout, "timeout", 30*time.Minute, "전체 타임아웃")
	flag.BoolVar(&opt.jsonOut, "json", false, "사람이 읽는 로그 대신 결과 요약 JSON만 stdout에 출력")
	flag.BoolVar(&opt.quiet, "quiet", false, "진행 로그 억제 (-json과 함께 쓰기 좋음)")
	flag.BoolVar(&opt.baseOnly, "baseonly", false, "베이스 캐릭터(base.png)만 생성하고 상태/번들은 건너뜀")
	flag.StringVar(&opt.framecounts, "framecounts", "", "프레임 수 오버라이드 JSON 경로 (Fruit War frame_counts.json 직접 소비, -unit 필요)")
	flag.StringVar(&opt.unit, "unit", "", "-framecounts JSON 내 유닛 키 (예: apple, banana)")
	flag.BoolVar(&opt.dumpraw, "dumpraw", false, "추출 전 clean 스트립을 <out>/raw/<state>.png 로 저장 (분할 진단용)")
	flag.StringVar(&opt.baseimg, "baseimg", "", "베이스를 생성하지 않고 이 이미지 파일을 base 로 사용 (GUI 업로드 재현)")
	flag.StringVar(&opt.extractfile, "extractfile", "", "생성 없이 이 raw 스트립 PNG 를 추출만 함 (-states 의 프레임 수로 분할; 분할 진단·재추출)")
	flag.Parse()

	if *dump {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"presets":    sprite.ListPresets(),
			"directions": sprite.ListDirections(),
			"styles":     []string{"pixel", "chibi", "cartoon", "retro16"},
			"providers":  gen.SupportedProviders,
		})
		return
	}

	if err := runGen(opt); err != nil {
		fmt.Fprintf(os.Stderr, "ppgen 실패: %v\n", err)
		os.Exit(1)
	}
}

// resolveProvider는 설정/환경변수를 읽고 CLI 오버라이드를 적용해 프로바이더를 만듭니다.
func resolveProvider(opt options) (gen.Provider, string, string, error) {
	s := config.Load()
	provider := s.Provider
	if opt.provider != "" {
		provider = opt.provider
	}
	cfg := s.Cfg(provider)
	key := cfg.APIKey
	if opt.key != "" {
		key = opt.key
	}
	model := cfg.Model
	if opt.model != "" {
		model = opt.model
	}
	if key == "" {
		return nil, "", "", fmt.Errorf("프로바이더 %q에 API 키가 없습니다 (config.json, .env, 환경변수 또는 -key 사용)", provider)
	}
	p, err := gen.New(provider, key, model)
	if err != nil {
		return nil, "", "", err
	}
	if model == "" {
		model = gen.DefaultModelFor(provider)
	}
	return p, provider, model, nil
}

// selectStates는 -all / -percat / -states 우선순위로 생성 대상 프리셋을 고릅니다.
func selectStates(opt options) ([]sprite.PresetInfo, error) {
	byName := map[string]sprite.PresetInfo{}
	for _, p := range sprite.Presets {
		byName[p.Name] = p
	}
	if opt.all {
		return append([]sprite.PresetInfo(nil), sprite.Presets...), nil
	}
	if opt.percat > 0 {
		count := map[string]int{}
		var out []sprite.PresetInfo
		for _, p := range sprite.Presets {
			if count[p.Category] < opt.percat {
				out = append(out, p)
				count[p.Category]++
			}
		}
		return out, nil
	}
	var out []sprite.PresetInfo
	var missing []string
	for _, n := range strings.Split(opt.states, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if p, ok := byName[n]; ok {
			out = append(out, p)
		} else {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("알 수 없는 상태 이름: %s (목록은 -dump 참고)", strings.Join(missing, ", "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("생성할 상태가 없습니다 (-states, -percat, 또는 -all 지정)")
	}
	return out, nil
}

// ppgenToGameState는 ppgen 프리셋 상태명 → Fruit War(frame_counts.json) 상태명 매핑입니다.
// ppgen_bridge.py 의 OUR_TO_PPGEN 역방향과 일관(hurt↔hit). 매핑에 없는 상태는 프리셋
// 이름을 그대로 게임 상태명으로 사용한다(idle/walk/attack/death/victory 동일).
var ppgenToGameState = map[string]string{
	"hurt": "hit",
}

// applyFrameCounts는 -framecounts JSON(Fruit War frame_counts.json)에서 -unit 의 프레임 수를
// 읽어 선택된 프리셋의 Frames 를 덮어씁니다. 유닛별 프리셋(presets.go) 수정 없이 범용화하는 용도.
//
// JSON 은 {"_comment": "...", "apple": {"idle":4, ...}, ...} 형태(게임 상태명 키). spawn 처럼
// ppgen 프리셋에 없는 상태나, 해당 유닛 dict 에 없는 상태는 건너뛴다(에러 아님).
func applyFrameCounts(presets []sprite.PresetInfo, opt options) ([]sprite.PresetInfo, error) {
	if strings.TrimSpace(opt.framecounts) == "" {
		return presets, nil
	}
	if strings.TrimSpace(opt.unit) == "" {
		return nil, fmt.Errorf("-framecounts 사용 시 -unit 도 지정해야 합니다 (예: -unit apple)")
	}
	raw, err := os.ReadFile(opt.framecounts)
	if err != nil {
		return nil, fmt.Errorf("-framecounts 파일 읽기 실패: %w", err)
	}
	// _comment 값이 문자열이라 map[string]map[string]int 로 한 번에 파싱하면 실패 → RawMessage 경유.
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("-framecounts JSON 파싱 실패: %w", err)
	}
	rawUnit, ok := all[opt.unit]
	if !ok {
		var keys []string
		for k := range all {
			if k == "_comment" {
				continue
			}
			keys = append(keys, k)
		}
		return nil, fmt.Errorf("-unit %q 가 %s 에 없습니다 (가능: %s)", opt.unit, opt.framecounts, strings.Join(keys, ", "))
	}
	var counts map[string]int
	if err := json.Unmarshal(rawUnit, &counts); err != nil {
		return nil, fmt.Errorf("-framecounts 의 %q 유닛 파싱 실패: %w", opt.unit, err)
	}

	out := make([]sprite.PresetInfo, len(presets))
	copy(out, presets)
	var applied, skipped []string
	for i := range out {
		gameState := out[i].Name
		if mapped, ok := ppgenToGameState[out[i].Name]; ok {
			gameState = mapped
		}
		if n, ok := counts[gameState]; ok && n > 0 {
			out[i].Frames = n
			applied = append(applied, fmt.Sprintf("%s=%d", out[i].Name, n))
		} else {
			skipped = append(skipped, out[i].Name)
		}
	}
	if !opt.quiet && !opt.jsonOut {
		fmt.Printf("[framecounts] %s 프레임 수 적용: %s", opt.unit, strings.Join(applied, " "))
		if len(skipped) > 0 {
			fmt.Printf(" (미적용: %s)", strings.Join(skipped, " "))
		}
		fmt.Println()
	}
	return out, nil
}
