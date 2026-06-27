# 설계: PerfectPixel Studio — 경량 프로젝트 목록

> 작성 2026-06-27. 대상 레포: `perfectpixel-studio`(포크, 브랜치 `fruitwar-doodle`).
> Wails v2 — Go 백엔드 + React/TS 프론트엔드. `fruit_war_flutter`(게임)는 변경 없음.

## 1. 배경 / 문제

PerfectPixel Studio GUI는 작업물을 **단일 세션 파일** 하나에만 자동 저장한다.

- 저장 경로: `~/Library/Application Support/perfectpixel/session.json` (`os.UserConfigDir()` 기준)
- 세션 형식: `{ v: 1, character, cellSize, states, selectedId }` — base64 PNG가 인라인이라 17MB+ 까지 커짐.
- 자동저장: 프론트(`App.tsx`)에서 `[character, cellSize, states, selectedId]` 변경 시 **1200ms 디바운스**로 `SaveSession(...)` 호출. 시작 시 `LoadSession()`으로 1회 복원(`restoredRef` 가드).
- **"새 프로젝트"(`handleNewProject` → `resetProject`)** 가 `ClearSession()`을 호출해 **session.json을 통째로 삭제**한다.

결과: 슬롯이 1개뿐이라 두 번째 유닛(banana 등)을 시작하려면 "새 프로젝트"를 눌러야 하고, 그 순간 이전 유닛(apple-infantry)의 **편집 가능한 프로젝트가 소실**된다. (게임으로 export된 `Apple_*.png` 자체는 안전하지만, 도구 안의 편집본은 사라짐.)

**목표:** 이름 있는 프로젝트를 여러 개 관리(생성/열기/이름변경/삭제)하고, 현재 작업 중인 apple-infantry 세션을 자동으로 1번 프로젝트로 보존한다.

### 범위 (확정)

- **포함:** 경량 프로젝트 목록 — 생성/열기/이름변경/삭제, 슬롯별 자동저장, 상단바 드롭다운 전환 UI, 기존 세션 자동 이관.
- **제외(YAGNI):** 썸네일 카드, 복제(duplicate), 프로젝트별 export 폴더 기억, 수동 정렬/드래그 재배치. Export·Gallery 동작은 불변.

## 2. 저장 모델 (Go)

단일 `session.json` → **프로젝트별 파일 + 메타 인덱스**로 전환한다.

```
~/Library/Application Support/perfectpixel/
├── projects/
│   ├── index.json          ← 메타만: { v, activeId, projects: [ProjectMeta...] }
│   ├── <id>.json           ← 프로젝트별 세션 페이로드 (기존 session.json과 동일 형식)
│   └── <id>.json …
├── session.json.migrated   ← 기존 session.json 을 리네임해 보존(안전망, 삭제 안 함)
├── config.json             ← 그대로
└── gallery/                ← 그대로
```

`ProjectMeta` (인덱스 항목):

```jsonc
{
  "id": "p-20260627-093015-7f3a",  // 발급값(타임스탬프+랜덤), 파일명 키
  "name": "apple-infantry",         // 표시·편집용. 이름변경은 인덱스만 수정(파일 이동 없음)
  "createdAt": "2026-06-27T09:30:15Z",
  "updatedAt": "2026-06-27T09:54:00Z"
}
```

- `<id>.json` 페이로드는 **기존 세션 형식 그대로** (`{ v, character, cellSize, states, selectedId }`) — 프론트 하이드레이션 로직 재사용.
- **인덱스는 메타만** 담아 가볍게 유지한다(`ListProjects`가 17MB 페이로드를 읽지 않게). 페이로드는 `LoadProject(id)`로 해당 파일을 열 때만 읽는다.
- 모든 쓰기는 기존 `SaveSession`과 동일하게 **tmp 파일 → rename** 원자적 교체(크래시 시 손상 방지).
- 경로 헬퍼는 `internal/config/config.go`에 `ProjectsDir()`, `ProjectIndexPath()`, `ProjectPath(id)` 추가.

## 3. 기존 세션 자동 이관 (1회, 서버 시작 시)

`projects/index.json`이 없고 레거시 `session.json`이 존재하면:

1. 새 `id` 발급 → `session.json` 내용을 `projects/<id>.json`으로 복사.
2. `name`은 페이로드의 `character.name`(예: `"apple-infantry"`), 비어 있으면 `"프로젝트 1"`. `createdAt`/`updatedAt`은 파일 mtime.
3. `index.json` 작성(`activeId` = 그 id).
4. 레거시 `session.json` → `session.json.migrated`로 **리네임**(삭제하지 않음 — 복구용 안전망).

→ 앱을 다시 열면 apple-infantry가 1번 프로젝트로 그대로 살아 있다. 이것이 "기존 내용 보존"의 핵심이며, 사용자가 별도 조치 없이 자동으로 적용된다.

이관은 **멱등(idempotent)** 해야 한다: `index.json`이 이미 있으면 이관 로직을 건너뛴다.

## 4. Go API (Wails 바인딩)

단일세션 메서드(`SaveSession`/`LoadSession`/`ClearSession`)를 다음으로 대체한다.

| 메서드 | 역할 |
|---|---|
| `ListProjects() []ProjectMeta` | 인덱스 반환, `updatedAt` 내림차순 정렬 |
| `LoadProject(id string) string` | 해당 프로젝트 세션 JSON 반환(없으면 빈 문자열) |
| `SaveProject(id, data string) error` | `<id>.json` 원자적 쓰기 + 인덱스 `updatedAt` 갱신 |
| `CreateProject(name string) (ProjectMeta, error)` | 새 id·빈 세션 생성, 인덱스 추가, 메타 반환 |
| `RenameProject(id, name string) error` | 인덱스 `name`만 수정 |
| `DeleteProject(id string) error` | `<id>.json` 삭제 + 인덱스 항목 제거 |
| `GetActiveProject() string` | `index.json`의 `activeId` 반환 |
| `SetActiveProject(id string) error` | `activeId` 저장(앱 재시작 시 마지막 프로젝트로 복귀) |

- 구 `SaveSession/LoadSession/ClearSession`은 **은퇴**한다. 자동저장은 `SaveProject(activeId, …)`로 대체되고, "새 프로젝트"의 `ClearSession` 호출은 제거된다. (호환을 위해 즉시 삭제하지 않고 미사용으로 남겨도 무방하나, 프론트에서 더 이상 호출하지 않음.)
- `DeleteProject`로 프로젝트가 0개가 되면 호출 측(프론트)이 곧바로 `CreateProject`로 빈 프로젝트를 만들어 **항상 ≥1** 불변식을 유지한다. (백엔드는 0개 상태 자체는 허용.)

## 5. 프론트엔드 (`App.tsx` + 드롭다운 컴포넌트)

### 상태 추가
`projects: ProjectMeta[]`, `activeId: string`, `loadingProject: boolean`.

### 시작 시
`LoadSession` 복원 블록을 교체: `ListProjects()` + `GetActiveProject()` → `LoadProject(active)` → **기존 하이드레이션 로직 재사용**(character/cellSize/states/selectedId 세팅, 구버전 `procedural`/`generating` 정리 그대로). 서버측 이관이 투명하게 선행되므로 첫 실행이면 apple-infantry가 active로 들어온다.

### 자동저장
`SaveSession(...)` → `SaveProject(activeId, ...)`. 동일한 1200ms 디바운스·`restoredRef` 가드 유지. 의존성 배열에 변화 없음(activeId는 ref로 참조).

### 프로젝트 전환 (드롭다운에서 선택)
경합 방지가 핵심:
1. `restoredRef.current = false` (디바운스 자동저장 잠금) + `loadingProject = true`.
2. 현재 프로젝트 즉시 저장(디바운스 flush → `SaveProject(activeId, 현재상태)` 직접 호출).
3. `SetActiveProject(targetId)` → `LoadProject(targetId)` → 하이드레이션.
4. `activeId = targetId`, `loadingProject = false`, `restoredRef.current = true`.

→ **디바운스가 새로 로드한 프로젝트 위에 이전 데이터를 덮어쓰지 않도록** 로드 완료 후에만 자동저장을 재개한다.

### 상단바 드롭다운 (신규 컴포넌트 `ProjectMenu.tsx`)
- 상단바에 `{activeName} ▾` 항상 표시.
- 클릭 → 목록(활성 항목에 ✓) + 구분선 + `✎ 이름변경` / `🗑 삭제`(활성 프로젝트 대상).
- 생성 중(`busy`)에는 드롭다운 비활성(다른 컨트롤과 동일 규칙).
- 구현은 기존 `components/ui/` 패턴(Dialog/Button/Input) 재사용. 별도 무거운 팝오버 라이브러리 도입 없음.

### "+ 새 프로젝트"
- 클릭 → 작은 **이름 입력 다이얼로그**(기존 `confirmNew` Dialog를 재활용).
- 확정 → 현재 프로젝트 flush 저장 → `CreateProject(name)` → 전환 로직(§ 전환)으로 빈 프로젝트로 이동.
- **기존 "작업이 사라집니다" 경고 모달 제거** — 이제 현재 프로젝트가 목록에 보존되므로 경고가 사실이 아니다.
- 이름 입력은 trim 후 빈 값이면 `"프로젝트 N"` 기본값. 중복 이름은 허용(파일 키는 id라 충돌 없음).

### 이름변경 / 삭제
- 이름변경 → 이름 입력 다이얼로그(새 프로젝트와 동일 컴포넌트, 기존 이름 prefill) → `RenameProject(id, name)` → 목록·표시명 갱신.
- 삭제 → 확인 다이얼로그 → `DeleteProject(id)`. 활성 프로젝트를 삭제했으면 남은 것 중 `updatedAt` 최신으로 전환. **0개가 되면** `CreateProject("프로젝트 1")` 후 그것으로 전환(§4 불변식).

## 6. i18n

`frontend/src/i18n/ui.{ko,en,zh,es}.ts` 4개 모두에 키 추가:

- `project_menu_tip`(드롭다운 툴팁), `rename`, `delete`,
- `confirm_delete_title`, `confirm_delete_desc`, `confirm_delete_ok`,
- `name_prompt_title`, `name_prompt_placeholder`, `name_prompt_ok`,
- `toast_project_created`, `toast_project_deleted`, `toast_project_renamed`, `toast_project_switched`.

`new_project`/`new_project_tip`은 유지(버튼 라벨 동일). `confirm_new_title`/`confirm_new_desc`/`confirm_new_ok`는 은퇴(이름 입력 다이얼로그가 대체) — 키 제거 또는 name_prompt로 재명명.

## 7. 테스트 / 검증

- **Go 단위 테스트**(`projects_test.go`, 기존 `app_test.go`·`gallery_test.go` 선례):
  - `CreateProject` → `ListProjects`(정렬·메타) → `LoadProject` 라운드트립.
  - `RenameProject`(인덱스만 변경, 페이로드 불변), `DeleteProject`(파일+인덱스 제거).
  - **이관**: 레거시 `session.json` 픽스처를 둔 임시 디렉토리에서 시작 → `index.json` 생성·`activeId` 세팅·`.migrated` 리네임·**멱등성**(재실행 시 무변화) 검증.
  - 원자적 쓰기(tmp→rename) 동작.
  - 테스트는 `os.UserConfigDir`를 환경변수(`XDG_CONFIG_HOME` 등)나 주입 가능한 경로로 격리 — 실제 사용자 디렉토리를 건드리지 않게 `config` 경로 헬퍼에 테스트 훅 검토.
- **프론트엔드**: 자동화 테스트 하네스 부재 → `wails dev` **수동 검증** 체크리스트:
  1. 첫 실행 시 apple-infantry가 active로 복원되는가.
  2. "+ 새 프로젝트"로 banana-archer 생성 → apple이 목록에 남아 있는가.
  3. 드롭다운으로 apple↔banana 전환 시 각자 상태가 온전한가(덮어쓰기 없음).
  4. 이름변경·삭제·0개→자동생성.
  5. 앱 재시작 후 마지막 active 프로젝트로 복귀.

## 8. 영향 / 리스크

- **데이터 안전:** 이관은 비파괴(원본 `.migrated` 보존). 멱등이라 재실행 안전.
- **대용량 페이로드:** 인덱스 분리로 목록 조회는 가볍고, 페이로드는 전환 시에만 로드. 디바운스 자동저장은 기존과 동일 부하.
- **경합:** 전환 중 자동저장 잠금(`restoredRef`/`loadingProject`)으로 교차 오염 방지 — 구현 시 가장 주의할 지점.
- **롤백:** 문제 시 `session.json.migrated`를 `session.json`으로 되돌리고 구 빌드 실행하면 복구 가능.

## 9. 변경 파일 요약

- `internal/config/config.go` — 경로 헬퍼 추가(`ProjectsDir`/`ProjectIndexPath`/`ProjectPath`).
- `app.go`(또는 신규 `projects.go`) — 8개 Wails 메서드 + 이관 로직, 시작 시 이관 호출.
- `projects_test.go` — Go 단위 테스트(신규).
- `frontend/src/App.tsx` — 시작 복원·자동저장·전환·새프로젝트·삭제 로직 교체.
- `frontend/src/components/ProjectMenu.tsx` — 상단바 드롭다운(신규).
- `frontend/src/types.ts` — `ProjectMeta` 타입.
- `frontend/src/i18n/ui.{ko,en,zh,es}.ts` — 키 추가/은퇴.
- `wailsjs/go/main/App.d.ts` 등 바인딩 — `wails generate` 재생성(빌드 산출물).
