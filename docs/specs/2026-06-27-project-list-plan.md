# 경량 프로젝트 목록 — 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** PerfectPixel Studio GUI에서 이름 있는 프로젝트를 여러 개 관리(생성/열기/이름변경/삭제)하고, 기존 apple-infantry 세션을 자동으로 첫 프로젝트로 보존한다.

**Architecture:** Go 백엔드는 단일 `session.json` 슬롯을 `projects/index.json`(메타) + `projects/<id>.json`(페이로드) 구조로 대체하고, 시작 시 레거시 세션을 1회 멱등 이관한다. 프론트엔드는 상단바 드롭다운으로 프로젝트를 전환하고, 자동저장 대상을 활성 프로젝트로 바꾼다.

**Tech Stack:** Wails v2, Go 1.25 (module `perfectpixel`, package `main`), React 18 + TypeScript, Vite.

**설계 출처:** `docs/specs/2026-06-27-project-list-design.md`

## Global Constraints

- 대상 레포: `perfectpixel-studio`, 브랜치 `fruitwar-doodle`. `fruit_war_flutter`(게임)는 변경 없음.
- Go 모듈명 `perfectpixel`, 루트 패키지 `main`. 내부 설정 패키지 `perfectpixel/internal/config`.
- 저장 루트: `os.UserConfigDir()/perfectpixel/`. 신규 데이터는 그 아래 `projects/`.
- **모든 파일 쓰기는 원자적(tmp 파일 → `os.Rename`)** 으로 한다(크래시 시 손상 방지).
- 코드 주석·문서·UI 문구는 **한국어** 기본. i18n 키는 4개 사전 모두(`ko`/`en`/`es`/`zh`)에 추가하되 누락 시 `en` → 키 순으로 폴백(`frontend/src/i18n/index.tsx`).
- **Go 테스트 격리:** `t.Setenv("HOME", t.TempDir())` (기존 `app_test.go` 선례 — darwin `os.UserConfigDir`가 `$HOME` 사용).
- **프론트 바인딩 경계:** Wails 생성 타입과 `types.ts` 인터페이스 불일치를 피하려 기존 코드처럼 `(x: any)`로 받는다(예: `App.tsx`의 `ListPresets().then((list: any) => …)`).
- 새 npm 의존성 추가 금지. 아이콘은 기존 `lucide-react` 사용.
- `wails` CLI가 PATH에 없으므로 바인딩 재생성은 `go run github.com/wailsapp/wails/v2/cmd/wails generate module` 사용(레포 루트에서).

---

### Task 1: Go 프로젝트 스토어 — 인덱스 + 핵심 CRUD

**Files:**
- Modify: `internal/config/config.go` (경로 헬퍼 3개 추가, 끝부분)
- Create: `projects.go` (레포 루트, package main)
- Test: `projects_test.go` (레포 루트, package main)

**Interfaces:**
- Produces:
  - `config.ProjectsDir() (string, error)`, `config.ProjectIndexPath() (string, error)`, `config.ProjectPath(id string) (string, error)`
  - 타입 `ProjectMeta struct { ID, Name, CreatedAt, UpdatedAt string }` (JSON: `id`,`name`,`createdAt`,`updatedAt`)
  - `(*App).ListProjects() []ProjectMeta` (updatedAt 내림차순)
  - `(*App).GetActiveProject() string`, `(*App).SetActiveProject(id string) error`
  - `(*App).LoadProject(id string) string`, `(*App).SaveProject(id, data string) error`
  - `(*App).CreateProject(name string) (ProjectMeta, error)` (새 프로젝트를 활성으로 지정)
  - 내부: `writeFileAtomic(path string, data []byte, perm os.FileMode) error`, `newProjectID() string`, `readIndex() (projectIndex, error)`, `writeIndex(projectIndex) error`

- [ ] **Step 1: config 경로 헬퍼 추가**

`internal/config/config.go` 끝(`GalleryDir` 함수 뒤, line 69 직후)에 추가:

```go
// ProjectsDir는 프로젝트별 세션 파일이 보관되는 디렉토리입니다.
func ProjectsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perfectpixel", "projects"), nil
}

// ProjectIndexPath는 프로젝트 메타 인덱스 파일 경로입니다.
func ProjectIndexPath() (string, error) {
	dir, err := ProjectsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "index.json"), nil
}

// ProjectPath는 주어진 id의 프로젝트 세션 파일 경로입니다.
func ProjectPath(id string) (string, error) {
	dir, err := ProjectsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}
```

- [ ] **Step 2: 실패하는 테스트 작성**

`projects_test.go` 생성:

```go
package main

import (
	"testing"
)

// TestProjectCRUD는 생성→목록→로드→저장→활성 전환의 핵심 흐름을 검증합니다.
func TestProjectCRUD(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := NewApp()

	// 초기: 빈 목록
	if got := a.ListProjects(); len(got) != 0 {
		t.Fatalf("초기 목록이 비어있지 않음: %d", len(got))
	}

	// 생성 → 활성 자동 지정
	meta, err := a.CreateProject("apple")
	if err != nil {
		t.Fatalf("CreateProject 실패: %v", err)
	}
	if meta.ID == "" || meta.Name != "apple" {
		t.Fatalf("메타 부정확: %+v", meta)
	}
	if a.GetActiveProject() != meta.ID {
		t.Fatalf("활성 프로젝트 미지정")
	}

	// 목록 1개
	list := a.ListProjects()
	if len(list) != 1 || list[0].ID != meta.ID {
		t.Fatalf("목록 불일치: %+v", list)
	}

	// 저장 → 로드 라운드트립
	payload := `{"v":1,"character":{"name":"apple"},"cellSize":256,"states":[]}`
	if err := a.SaveProject(meta.ID, payload); err != nil {
		t.Fatalf("SaveProject 실패: %v", err)
	}
	if got := a.LoadProject(meta.ID); got != payload {
		t.Fatalf("로드 불일치: %q", got)
	}

	// 두 번째 생성 → 활성 전환, 목록 2개
	meta2, err := a.CreateProject("banana")
	if err != nil {
		t.Fatalf("두번째 생성 실패: %v", err)
	}
	if a.GetActiveProject() != meta2.ID {
		t.Fatalf("두번째 활성 전환 안됨")
	}
	if len(a.ListProjects()) != 2 {
		t.Fatalf("목록 2개 아님")
	}

	// SetActiveProject
	if err := a.SetActiveProject(meta.ID); err != nil {
		t.Fatalf("SetActive 실패: %v", err)
	}
	if a.GetActiveProject() != meta.ID {
		t.Fatalf("활성 전환 실패")
	}
}
```

- [ ] **Step 3: 테스트 실패 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio && go test . -run TestProjectCRUD -v`
Expected: 컴파일 실패 (`undefined: (*App).ListProjects` 등)

- [ ] **Step 4: `projects.go` 구현**

`projects.go` 생성:

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"perfectpixel/internal/config"
)

// ProjectMeta는 프로젝트 인덱스 항목(메타데이터만)입니다.
type ProjectMeta struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// projectIndex는 projects/index.json 의 구조입니다.
type projectIndex struct {
	V        int           `json:"v"`
	ActiveID string        `json:"activeId"`
	Projects []ProjectMeta `json:"projects"`
}

var projectSeq atomic.Uint64

func newProjectID() string {
	n := projectSeq.Add(1)
	return fmt.Sprintf("p-%s-%d", time.Now().Format("20060102-150405"), n)
}

func nowStamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// writeFileAtomic은 tmp 파일에 쓴 뒤 rename으로 교체해 손상을 방지합니다.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readIndex() (projectIndex, error) {
	path, err := config.ProjectIndexPath()
	if err != nil {
		return projectIndex{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return projectIndex{V: 1}, nil
		}
		return projectIndex{}, err
	}
	var idx projectIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return projectIndex{}, err
	}
	if idx.V == 0 {
		idx.V = 1
	}
	return idx, nil
}

func writeIndex(idx projectIndex) error {
	path, err := config.ProjectIndexPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// ListProjects는 프로젝트 메타 목록을 updatedAt 내림차순으로 반환합니다.
func (a *App) ListProjects() []ProjectMeta {
	idx, err := readIndex()
	if err != nil {
		return []ProjectMeta{}
	}
	out := make([]ProjectMeta, len(idx.Projects))
	copy(out, idx.Projects)
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// GetActiveProject는 현재 활성 프로젝트 id를 반환합니다(없으면 빈 문자열).
func (a *App) GetActiveProject() string {
	idx, _ := readIndex()
	return idx.ActiveID
}

// SetActiveProject는 활성 프로젝트 id를 저장합니다.
func (a *App) SetActiveProject(id string) error {
	idx, err := readIndex()
	if err != nil {
		return err
	}
	idx.ActiveID = id
	return writeIndex(idx)
}

// LoadProject는 프로젝트 세션 JSON을 반환합니다(없으면 빈 문자열).
func (a *App) LoadProject(id string) string {
	path, err := config.ProjectPath(id)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveProject는 프로젝트 세션을 원자적으로 저장하고 인덱스 updatedAt을 갱신합니다.
func (a *App) SaveProject(id, data string) error {
	if id == "" {
		return errors.New("프로젝트 id가 비어 있습니다")
	}
	path, err := config.ProjectPath(id)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(data), 0o600); err != nil {
		return err
	}
	idx, err := readIndex()
	if err != nil {
		return err
	}
	for i := range idx.Projects {
		if idx.Projects[i].ID == id {
			idx.Projects[i].UpdatedAt = nowStamp()
			return writeIndex(idx)
		}
	}
	return nil // 인덱스에 없으면(이론상) 무시
}

// CreateProject는 빈 프로젝트를 만들고 활성으로 지정한 뒤 메타를 반환합니다.
func (a *App) CreateProject(name string) (ProjectMeta, error) {
	idx, err := readIndex()
	if err != nil {
		return ProjectMeta{}, err
	}
	now := nowStamp()
	meta := ProjectMeta{ID: newProjectID(), Name: name, CreatedAt: now, UpdatedAt: now}
	path, err := config.ProjectPath(meta.ID)
	if err != nil {
		return ProjectMeta{}, err
	}
	if err := writeFileAtomic(path, []byte("{}"), 0o600); err != nil {
		return ProjectMeta{}, err
	}
	idx.Projects = append(idx.Projects, meta)
	idx.ActiveID = meta.ID
	if err := writeIndex(idx); err != nil {
		return ProjectMeta{}, err
	}
	return meta, nil
}
```

- [ ] **Step 5: 테스트 통과 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio && go test . -run TestProjectCRUD -v`
Expected: PASS

- [ ] **Step 6: 커밋**

```bash
cd ~/StudioProjects/perfectpixel-studio
git add internal/config/config.go projects.go projects_test.go
git commit -m "feat(projects): 프로젝트 스토어 인덱스 + 핵심 CRUD

ProjectsDir/ProjectIndexPath/ProjectPath 경로 헬퍼,
ListProjects/CreateProject/LoadProject/SaveProject/Get·SetActiveProject,
원자적 쓰기 + updatedAt 내림차순 목록."
```

---

### Task 2: Go — 이름변경 / 삭제

**Files:**
- Modify: `projects.go`
- Test: `projects_test.go`

**Interfaces:**
- Consumes: Task 1의 `readIndex`/`writeIndex`/`config.ProjectPath`/`nowStamp`
- Produces:
  - `(*App).RenameProject(id, name string) error`
  - `(*App).DeleteProject(id string) error` (활성 삭제 시 남은 것 중 updatedAt 최신으로 재지정; 0개 허용)

- [ ] **Step 1: 실패하는 테스트 작성**

`projects_test.go`에 추가:

```go
// TestProjectRenameDelete는 이름변경과 삭제(비활성/활성/마지막)를 검증합니다.
func TestProjectRenameDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := NewApp()
	m1, _ := a.CreateProject("a")
	m2, _ := a.CreateProject("b") // 활성 = m2, 목록 [m1, m2]

	// 이름변경
	if err := a.RenameProject(m1.ID, "apple-infantry"); err != nil {
		t.Fatalf("rename 실패: %v", err)
	}
	renamed := false
	for _, p := range a.ListProjects() {
		if p.ID == m1.ID && p.Name == "apple-infantry" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatalf("이름변경 반영 안됨")
	}

	// 없는 id 이름변경 → 에러
	if err := a.RenameProject("nope", "x"); err == nil {
		t.Fatalf("없는 프로젝트 이름변경이 에러 아님")
	}

	// 비활성(m1) 삭제 → 목록 1개, 페이로드 제거
	if err := a.DeleteProject(m1.ID); err != nil {
		t.Fatalf("delete 실패: %v", err)
	}
	if len(a.ListProjects()) != 1 {
		t.Fatalf("삭제 후 1개 아님")
	}
	if a.LoadProject(m1.ID) != "" {
		t.Fatalf("삭제된 페이로드 남아있음")
	}
	if a.GetActiveProject() != m2.ID {
		t.Fatalf("비활성 삭제인데 활성 바뀜")
	}

	// 활성(m3) 삭제 → 남은 최신(m2)으로 재지정
	m3, _ := a.CreateProject("c") // 활성 = m3, 목록 [m2, m3]
	if err := a.DeleteProject(m3.ID); err != nil {
		t.Fatalf("활성 삭제 실패: %v", err)
	}
	if a.GetActiveProject() != m2.ID {
		t.Fatalf("활성 재지정 실패: %s", a.GetActiveProject())
	}

	// 마지막 삭제 → 0개 허용, 활성 빈 문자열
	if err := a.DeleteProject(m2.ID); err != nil {
		t.Fatalf("마지막 삭제 실패: %v", err)
	}
	if len(a.ListProjects()) != 0 {
		t.Fatalf("0개 아님")
	}
	if a.GetActiveProject() != "" {
		t.Fatalf("활성이 비어있지 않음: %s", a.GetActiveProject())
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio && go test . -run TestProjectRenameDelete -v`
Expected: 컴파일 실패 (`undefined: (*App).RenameProject`)

- [ ] **Step 3: 구현 추가**

`projects.go` 끝에 추가:

```go
// RenameProject는 프로젝트 표시 이름을 변경합니다(페이로드 불변).
func (a *App) RenameProject(id, name string) error {
	idx, err := readIndex()
	if err != nil {
		return err
	}
	for i := range idx.Projects {
		if idx.Projects[i].ID == id {
			idx.Projects[i].Name = name
			idx.Projects[i].UpdatedAt = nowStamp()
			return writeIndex(idx)
		}
	}
	return fmt.Errorf("프로젝트를 찾을 수 없습니다: %s", id)
}

// DeleteProject는 프로젝트 파일과 인덱스 항목을 제거합니다.
// 활성 프로젝트를 지우면 남은 것 중 updatedAt 최신으로 활성을 재지정합니다(없으면 빈 문자열).
func (a *App) DeleteProject(id string) error {
	idx, err := readIndex()
	if err != nil {
		return err
	}
	found := false
	next := make([]ProjectMeta, 0, len(idx.Projects))
	for _, p := range idx.Projects {
		if p.ID == id {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return fmt.Errorf("프로젝트를 찾을 수 없습니다: %s", id)
	}
	idx.Projects = next
	if idx.ActiveID == id {
		idx.ActiveID = ""
		for _, p := range idx.Projects {
			if idx.ActiveID == "" || p.UpdatedAt > metaByID(idx.Projects, idx.ActiveID).UpdatedAt {
				idx.ActiveID = p.ID
			}
		}
	}
	if err := writeIndex(idx); err != nil {
		return err
	}
	path, err := config.ProjectPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func metaByID(list []ProjectMeta, id string) ProjectMeta {
	for _, p := range list {
		if p.ID == id {
			return p
		}
	}
	return ProjectMeta{}
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio && go test . -run TestProjectRenameDelete -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
cd ~/StudioProjects/perfectpixel-studio
git add projects.go projects_test.go
git commit -m "feat(projects): 프로젝트 이름변경·삭제

활성 삭제 시 남은 최신으로 활성 재지정, 0개 허용."
```

---

### Task 3: Go — 레거시 세션 자동 이관 + startup 배선

**Files:**
- Modify: `projects.go` (이관 함수)
- Modify: `app.go:39-41` (startup에서 이관 호출)
- Test: `projects_test.go`

**Interfaces:**
- Consumes: Task 1의 `readIndex`/`writeIndex`/`writeFileAtomic`/`newProjectID`/`config.SessionPath`/`config.ProjectIndexPath`/`config.ProjectPath`
- Produces: `(*App).migrateLegacySession() error` (멱등), `projectNameFromSession([]byte) string`

- [ ] **Step 1: 실패하는 테스트 작성**

`projects_test.go`에 추가(상단 import에 `os`, `path/filepath`, `perfectpixel/internal/config` 필요 — import 블록을 아래로 교체):

```go
import (
	"os"
	"path/filepath"
	"testing"

	"perfectpixel/internal/config"
)
```

테스트 함수 추가:

```go
// TestMigrateLegacySession은 레거시 session.json이 첫 프로젝트로 멱등 이관되는지 검증합니다.
func TestMigrateLegacySession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := NewApp()

	sessPath, err := config.SessionPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sessPath), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"v":1,"character":{"name":"apple-infantry"},"cellSize":256,"states":[]}`
	if err := os.WriteFile(sessPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.migrateLegacySession(); err != nil {
		t.Fatalf("이관 실패: %v", err)
	}

	list := a.ListProjects()
	if len(list) != 1 || list[0].Name != "apple-infantry" {
		t.Fatalf("이관 프로젝트 부정확: %+v", list)
	}
	if a.GetActiveProject() != list[0].ID {
		t.Fatalf("활성 미지정")
	}
	if got := a.LoadProject(list[0].ID); got != payload {
		t.Fatalf("페이로드 불일치: %q", got)
	}
	if _, err := os.Stat(sessPath); !os.IsNotExist(err) {
		t.Fatalf("원본 session.json이 남아있음")
	}
	if _, err := os.Stat(sessPath + ".migrated"); err != nil {
		t.Fatalf(".migrated 백업 없음: %v", err)
	}

	// 멱등성: 재실행해도 변화 없음
	if err := a.migrateLegacySession(); err != nil {
		t.Fatalf("재이관 실패: %v", err)
	}
	if len(a.ListProjects()) != 1 {
		t.Fatalf("재이관 후 프로젝트 수 변함: %d", len(a.ListProjects()))
	}
}

// TestMigrateNoLegacy는 레거시 세션이 없을 때 빈 인덱스만 생성됨을 검증합니다.
func TestMigrateNoLegacy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := NewApp()
	if err := a.migrateLegacySession(); err != nil {
		t.Fatalf("이관 실패: %v", err)
	}
	if len(a.ListProjects()) != 0 {
		t.Fatalf("레거시 없는데 프로젝트 생김")
	}
	idxPath, _ := config.ProjectIndexPath()
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("인덱스 미생성: %v", err)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio && go test . -run TestMigrate -v`
Expected: 컴파일 실패 (`undefined: (*App).migrateLegacySession`)

- [ ] **Step 3: 이관 구현 추가**

`projects.go`에 import `"strings"` 추가 후, 끝에 추가:

```go
// migrateLegacySession은 projects 인덱스가 없을 때 레거시 session.json을
// 첫 프로젝트로 이관합니다. 인덱스가 이미 있으면 아무것도 하지 않습니다(멱등).
func (a *App) migrateLegacySession() error {
	idxPath, err := config.ProjectIndexPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(idxPath); err == nil {
		return nil // 이미 인덱스 존재 → 멱등 종료
	}

	sessPath, err := config.SessionPath()
	if err != nil {
		return err
	}
	info, statErr := os.Stat(sessPath)
	if statErr != nil {
		// 레거시 세션 없음 → 빈 인덱스만 생성해 이후 멱등 보장
		return writeIndex(projectIndex{V: 1})
	}

	data, err := os.ReadFile(sessPath)
	if err != nil {
		return err
	}
	now := info.ModTime().UTC().Format(time.RFC3339)
	meta := ProjectMeta{
		ID:        newProjectID(),
		Name:      projectNameFromSession(data),
		CreatedAt: now,
		UpdatedAt: now,
	}
	pPath, err := config.ProjectPath(meta.ID)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(pPath, data, 0o600); err != nil {
		return err
	}
	if err := writeIndex(projectIndex{V: 1, ActiveID: meta.ID, Projects: []ProjectMeta{meta}}); err != nil {
		return err
	}
	// 원본 보존(삭제 대신 리네임)
	_ = os.Rename(sessPath, sessPath+".migrated")
	return nil
}

// projectNameFromSession은 세션 페이로드의 character.name을 프로젝트 이름으로 씁니다.
func projectNameFromSession(data []byte) string {
	var s struct {
		Character struct {
			Name string `json:"name"`
		} `json:"character"`
	}
	if err := json.Unmarshal(data, &s); err == nil {
		if n := strings.TrimSpace(s.Character.Name); n != "" {
			return n
		}
	}
	return "프로젝트 1"
}
```

- [ ] **Step 4: startup에서 이관 호출**

`app.go:39-41`의 `startup`을 교체:

```go
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.migrateLegacySession(); err != nil {
		// 이관 실패는 치명적이지 않음 — 로그만 남기고 계속
		fmt.Println("프로젝트 이관 실패:", err)
	}
}
```

(`fmt`는 `app.go`에 이미 import됨.)

- [ ] **Step 5: 테스트 통과 + 전체 회귀 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio && go test ./... -v`
Expected: PASS (신규 `TestMigrate*` + 기존 `TestSessionRoundTrip`/`TestGallery*` 등 모두)

- [ ] **Step 6: 커밋**

```bash
cd ~/StudioProjects/perfectpixel-studio
git add projects.go projects_test.go app.go
git commit -m "feat(projects): 레거시 session.json 자동 이관 + startup 배선

인덱스 부재 시 1회 멱등 이관(character.name → 프로젝트명,
원본은 .migrated로 보존), startup에서 호출."
```

---

### Task 4: Wails 바인딩 재생성 + i18n 키 추가 + ProjectMeta 타입

**Files:**
- Regenerate: `frontend/wailsjs/go/main/App.{js,d.ts}`, `frontend/wailsjs/go/models.ts`
- Modify: `frontend/src/types.ts` (ProjectMeta 인터페이스 추가)
- Modify: `frontend/src/i18n/ui.ko.ts`, `ui.en.ts`, `ui.es.ts`, `ui.zh.ts`

**Interfaces:**
- Consumes: Task 1~3의 바운드 메서드(`ListProjects` 등)
- Produces: 프론트에서 import 가능한 8개 바인딩 함수, `ProjectMeta` TS 인터페이스, 신규 i18n 키

- [ ] **Step 1: 바인딩 재생성**

Run:
```bash
cd ~/StudioProjects/perfectpixel-studio
go run github.com/wailsapp/wails/v2/cmd/wails generate module
```
Expected: 에러 없이 완료. 다음으로 확인:
```bash
grep -o "CreateProject\|ListProjects\|DeleteProject\|RenameProject\|LoadProject\|SaveProject\|GetActiveProject\|SetActiveProject" frontend/wailsjs/go/main/App.d.ts | sort -u
```
Expected: 8개 메서드 이름 모두 출력. `grep ProjectMeta frontend/wailsjs/go/models.ts` 도 매칭되어야 함.

- [ ] **Step 2: ProjectMeta 인터페이스 추가**

`frontend/src/types.ts`의 `PresetInfo` 인터페이스 블록(line 63~72) 뒤에 추가:

```typescript
// 백엔드 main.ProjectMeta와 동일 구조 (ListProjects/CreateProject 응답)
export interface ProjectMeta {
  id: string;
  name: string;
  createdAt: string;
  updatedAt: string;
}
```

- [ ] **Step 3: i18n 키 추가 (4개 사전)**

각 파일에서 `new_project_tip` 값을 갱신하고, 그 줄 **뒤에** 신규 키 블록을 삽입한다.

`frontend/src/i18n/ui.ko.ts` — `new_project_tip` 값 교체 + 삽입:
```typescript
  new_project_tip: "현재 프로젝트는 보존하고 새 프로젝트를 시작",
  default_project_name: "프로젝트 1",
  project_menu_tip: "프로젝트 전환 · 이름변경 · 삭제",
  rename: "이름변경",
  delete: "삭제",
  name_prompt_title_new: "새 프로젝트 이름",
  name_prompt_title_rename: "프로젝트 이름 변경",
  name_prompt_placeholder: "예: banana-archer",
  name_prompt_ok: "확인",
  confirm_delete_title: "프로젝트를 삭제할까요?",
  confirm_delete_desc: "'{name}' 프로젝트가 영구히 삭제됩니다. 이 작업은 되돌릴 수 없습니다.",
  confirm_delete_ok: "삭제",
  toast_project_created: "프로젝트 '{name}' 생성됨",
  toast_project_renamed: "이름이 변경되었습니다.",
  toast_project_deleted: "프로젝트가 삭제되었습니다.",
```

`frontend/src/i18n/ui.en.ts`:
```typescript
  new_project_tip: "Keep the current project and start a new one",
  default_project_name: "Project 1",
  project_menu_tip: "Switch · rename · delete project",
  rename: "Rename",
  delete: "Delete",
  name_prompt_title_new: "New project name",
  name_prompt_title_rename: "Rename project",
  name_prompt_placeholder: "e.g. banana-archer",
  name_prompt_ok: "OK",
  confirm_delete_title: "Delete this project?",
  confirm_delete_desc: "Project '{name}' will be permanently deleted. This cannot be undone.",
  confirm_delete_ok: "Delete",
  toast_project_created: "Project '{name}' created",
  toast_project_renamed: "Project renamed.",
  toast_project_deleted: "Project deleted.",
```

`frontend/src/i18n/ui.es.ts`:
```typescript
  new_project_tip: "Conserva el proyecto actual y empieza uno nuevo",
  default_project_name: "Proyecto 1",
  project_menu_tip: "Cambiar · renombrar · eliminar proyecto",
  rename: "Renombrar",
  delete: "Eliminar",
  name_prompt_title_new: "Nombre del nuevo proyecto",
  name_prompt_title_rename: "Renombrar proyecto",
  name_prompt_placeholder: "p. ej. banana-archer",
  name_prompt_ok: "Aceptar",
  confirm_delete_title: "¿Eliminar este proyecto?",
  confirm_delete_desc: "El proyecto '{name}' se eliminará permanentemente. Esta acción no se puede deshacer.",
  confirm_delete_ok: "Eliminar",
  toast_project_created: "Proyecto '{name}' creado",
  toast_project_renamed: "Proyecto renombrado.",
  toast_project_deleted: "Proyecto eliminado.",
```

`frontend/src/i18n/ui.zh.ts`:
```typescript
  new_project_tip: "保留当前项目并新建一个",
  default_project_name: "项目 1",
  project_menu_tip: "切换 · 重命名 · 删除项目",
  rename: "重命名",
  delete: "删除",
  name_prompt_title_new: "新项目名称",
  name_prompt_title_rename: "重命名项目",
  name_prompt_placeholder: "例如 banana-archer",
  name_prompt_ok: "确定",
  confirm_delete_title: "删除此项目？",
  confirm_delete_desc: "项目 '{name}' 将被永久删除。此操作无法撤销。",
  confirm_delete_ok: "删除",
  toast_project_created: "已创建项目 '{name}'",
  toast_project_renamed: "项目已重命名。",
  toast_project_deleted: "项目已删除。",
```

(각 파일의 기존 `new_project_tip` 줄을 위 블록의 첫 줄로 교체하면서 나머지 키를 그 아래에 붙인다. `confirm_new_*` 키는 Task 6에서 제거.)

- [ ] **Step 4: 타입체크 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio/frontend && npx tsc --noEmit`
Expected: 에러 없음 (신규 키·타입·바인딩은 아직 미사용이지만 컴파일은 통과)

- [ ] **Step 5: 커밋**

```bash
cd ~/StudioProjects/perfectpixel-studio
git add frontend/wailsjs frontend/src/types.ts frontend/src/i18n
git commit -m "feat(projects): Wails 바인딩 재생성 + i18n 키 + ProjectMeta 타입

8개 프로젝트 메서드 바인딩, 4개 사전에 프로젝트 관련 문구 추가."
```

---

### Task 5: 상단바 프로젝트 드롭다운 컴포넌트

**Files:**
- Create: `frontend/src/components/ProjectMenu.tsx`
- Modify: `frontend/src/style.css` (드롭다운 스타일 추가, 파일 끝)

**Interfaces:**
- Consumes: `ProjectMeta`(types.ts), i18n 키(Task 4)
- Produces: `ProjectMenu` 컴포넌트 — props `{ projects: ProjectMeta[]; activeId: string; busy: boolean; onSwitch: (id: string) => void; onRename: (id: string) => void; onDelete: (id: string) => void }`

- [ ] **Step 1: 컴포넌트 작성**

`frontend/src/components/ProjectMenu.tsx` 생성:

```tsx
import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Pencil, Trash2 } from "lucide-react";
import { Button } from "./ui/button";
import { useI18n } from "../i18n";
import { ProjectMeta } from "../types";

interface Props {
  projects: ProjectMeta[];
  activeId: string;
  busy: boolean;
  onSwitch: (id: string) => void;
  onRename: (id: string) => void;
  onDelete: (id: string) => void;
}

export default function ProjectMenu({ projects, activeId, busy, onSwitch, onRename, onDelete }: Props) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const active = projects.find((p) => p.id === activeId);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  return (
    <div className="project-menu" ref={ref}>
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        onClick={() => setOpen((o) => !o)}
        title={t("project_menu_tip")}
      >
        {active?.name ?? t("default_project_name")} <ChevronDown size={13} />
      </Button>
      {open && (
        <div className="project-menu-pop">
          {projects.map((p) => (
            <button
              key={p.id}
              className="project-menu-item"
              onClick={() => {
                setOpen(false);
                onSwitch(p.id);
              }}
            >
              {p.id === activeId ? <Check size={12} /> : <span className="project-menu-gap" />}
              <span className="project-menu-name">{p.name}</span>
            </button>
          ))}
          <div className="project-menu-sep" />
          <button
            className="project-menu-item"
            onClick={() => {
              setOpen(false);
              onRename(activeId);
            }}
          >
            <Pencil size={12} /> <span className="project-menu-name">{t("rename")}</span>
          </button>
          <button
            className="project-menu-item project-menu-danger"
            onClick={() => {
              setOpen(false);
              onDelete(activeId);
            }}
          >
            <Trash2 size={12} /> <span className="project-menu-name">{t("delete")}</span>
          </button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: 드롭다운 스타일 추가**

`frontend/src/style.css` 끝에 추가:

```css
/* 프로젝트 드롭다운 */
.project-menu {
  position: relative;
}
.project-menu-pop {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  z-index: 60;
  min-width: 200px;
  padding: 4px;
  border-radius: 10px;
  background: #1a1c26;
  border: 1px solid rgba(255, 255, 255, 0.12);
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
}
.project-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: #e8e9ef;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}
.project-menu-item:hover {
  background: rgba(255, 255, 255, 0.08);
}
.project-menu-danger {
  color: #ff6b6b;
}
.project-menu-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-menu-gap {
  display: inline-block;
  width: 12px;
}
.project-menu-sep {
  height: 1px;
  margin: 4px 2px;
  background: rgba(255, 255, 255, 0.1);
}
```

- [ ] **Step 3: 타입체크 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio/frontend && npx tsc --noEmit`
Expected: 에러 없음 (`ProjectMenu`는 아직 미사용이지만 컴파일 통과)

- [ ] **Step 4: 커밋**

```bash
cd ~/StudioProjects/perfectpixel-studio
git add frontend/src/components/ProjectMenu.tsx frontend/src/style.css
git commit -m "feat(projects): 상단바 프로젝트 드롭다운 컴포넌트

전환(✓ 표시)·이름변경·삭제 항목, 클릭 외부 닫힘."
```

---

### Task 6: App.tsx 통합 — 세션 API 교체·전환·다이얼로그·상단바

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/i18n/ui.{ko,en,es,zh}.ts` (`confirm_new_*` 제거)

**Interfaces:**
- Consumes: Task 1~5 전부 (바인딩 8개, `ProjectMeta`, `ProjectMenu`, i18n 키)

> 이 태스크는 단위 테스트 하네스가 없어 **타입체크(`npx tsc --noEmit`) + 수동 검증**으로 마감한다. 각 편집 단계 후 마지막에 한 번 타입체크한다.

- [ ] **Step 1: import 교체**

`App.tsx:2` (lucide) 와 `App.tsx:3` (바인딩), `App.tsx:11~12` 를 아래로 교체:

```tsx
import { Images, Package, Plus, Settings, X } from "lucide-react";
import { CancelGeneration, CreateProject, DeleteProject, ExportProject, GenerateState, GetActiveProject, GetSettings, ListDirections, ListPresets, ListProjects, LoadProject, MirrorFrames, ReExtractState, RenameProject, RevealInFinder, SaveProject, SetActiveProject } from "../wailsjs/go/main/App";
```

`App.tsx:11~12`의 두 import 줄을 아래로 교체(`Input`, `ProjectMenu`, `ProjectMeta` 추가):

```tsx
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./components/ui/dialog";
import { Input } from "./components/ui/input";
import ProjectMenu from "./components/ProjectMenu";
import { CharacterDef, DirectionInfo, FALLBACK_PRESETS, FrameItem, PresetInfo, ProjectMeta, StateDef, selectedFrames, uid } from "./types";
```

(즉 `ClearSession`, `LoadSession`, `SaveSession` 제거; `CreateProject`/`DeleteProject`/`GetActiveProject`/`ListProjects`/`LoadProject`/`RenameProject`/`SaveProject`/`SetActiveProject` 추가.)

- [ ] **Step 2: 상태/ref 교체 — confirmNew 제거, 프로젝트 상태 추가**

`App.tsx:38` 의 `const [confirmNew, setConfirmNew] = useState(false);` 줄을 아래로 교체:

```tsx
  const [projects, setProjects] = useState<ProjectMeta[]>([]);
  const [activeId, setActiveId] = useState<string>("");
  const [nameDialog, setNameDialog] = useState<{ open: boolean; mode: "new" | "rename"; value: string; targetId: string }>({ open: false, mode: "new", value: "", targetId: "" });
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id: string }>({ open: false, id: "" });
```

`App.tsx:50` 의 `selectedId` useState 뒤(`App.tsx:64` busyRef 부근)에 ref 추가. 구체적으로 `App.tsx:66` 의 `restoredRef` 선언 **앞**에 삽입:

```tsx
  const selectedIdRef = useRef(selectedId);
  selectedIdRef.current = selectedId;
  const activeIdRef = useRef(activeId);
  activeIdRef.current = activeId;
```

- [ ] **Step 3: applySession 헬퍼 추가 + 시작 복원 교체**

`App.tsx:84~116` 의 `// 이전 작업 세션 복원` 블록(즉시실행 async 함수 `(async () => { … })();`)을 아래로 교체:

```tsx
    // 프로젝트 목록 로드 + 활성 프로젝트 복원
    (async () => {
      try {
        const list: any = await ListProjects();
        let active: string = await GetActiveProject();
        if (!Array.isArray(list) || list.length === 0) {
          const meta: any = await CreateProject(t("default_project_name"));
          setProjects([meta]);
          setActiveId(meta.id);
          applySession("");
        } else {
          if (!active || !list.some((p: ProjectMeta) => p.id === active)) active = list[0].id;
          setProjects(list);
          setActiveId(active);
          applySession(await LoadProject(active));
          await SetActiveProject(active);
        }
      } catch {
        // 무시 (다음 편집의 자동저장이 복구)
      } finally {
        restoredRef.current = true;
      }
    })();
```

그리고 `applySession` 헬퍼를 컴포넌트 함수 본문(예: `handleCancel` 정의 뒤, `App.tsx:488` 부근)에 추가:

```tsx
  // 세션 JSON을 화면 상태로 적용(빈 문자열이면 기본값으로 리셋).
  const applySession = (raw: string) => {
    setCharacter({ image: null, name: "", description: "", styleKey: "cartoon", styleCustom: "" });
    setCellSize(256);
    setStates([]);
    setSelectedId(null);
    if (!raw) return;
    try {
      const s = JSON.parse(raw);
      if (s?.character) {
        setCharacter({
          image: s.character.image ?? null,
          name: s.character.name ?? "",
          description: s.character.description ?? "",
          styleKey: s.character.styleKey ?? "cartoon",
          styleCustom: s.character.styleCustom ?? "",
        });
      }
      if (typeof s?.cellSize === "number") setCellSize(s.cellSize);
      if (Array.isArray(s?.states)) {
        const next: StateDef[] = s.states
          .filter((st: any) => st?.mode !== "procedural")
          .map((st: StateDef) => ({
            ...st,
            status: st.status === "generating" ? (st.items?.length > 0 ? "done" : "idle") : st.status,
          }));
        setStates(next);
        if (s.selectedId && next.some((x) => x.id === s.selectedId)) setSelectedId(s.selectedId);
      }
    } catch {
      // 손상된 세션 무시
    }
  };
```

- [ ] **Step 4: 자동저장 대상을 활성 프로젝트로 교체**

`App.tsx:122~128` 의 자동저장 useEffect를 아래로 교체:

```tsx
  // 활성 프로젝트 자동 저장 (디바운스)
  useEffect(() => {
    if (!restoredRef.current) return;
    const id = activeIdRef.current;
    if (!id) return;
    const tm = setTimeout(() => {
      SaveProject(id, JSON.stringify({ v: 1, character, cellSize, states, selectedId })).catch(() => {});
    }, 1200);
    return () => clearTimeout(tm);
  }, [character, cellSize, states, selectedId]);
```

- [ ] **Step 5: 전환/생성/이름변경/삭제 플로우 + 핸들러 교체**

`App.tsx:490~514` 의 `handleNewProject` + `resetProject` 두 함수를 아래로 통째 교체:

```tsx
  // 현재 프로젝트를 즉시(디바운스 없이) 저장
  const flushSaveCurrent = async () => {
    const id = activeIdRef.current;
    if (!id) return;
    try {
      await SaveProject(id, JSON.stringify({
        v: 1,
        character: charRef.current,
        cellSize: cellRef.current,
        states: statesRef.current,
        selectedId: selectedIdRef.current,
      }));
    } catch {
      // 저장 실패는 무시
    }
  };

  const switchProject = async (id: string) => {
    if (busyRef.current || id === activeIdRef.current) return;
    restoredRef.current = false; // 전환 중 자동저장 잠금
    await flushSaveCurrent();
    try {
      await SetActiveProject(id);
      const raw = await LoadProject(id);
      setActiveId(id);
      applySession(raw);
    } finally {
      setTimeout(() => {
        restoredRef.current = true;
      }, 0);
    }
  };

  const createProjectFlow = async (name: string) => {
    restoredRef.current = false;
    await flushSaveCurrent();
    try {
      const meta: any = await CreateProject(name.trim() || t("default_project_name"));
      setProjects(await ListProjects());
      setActiveId(meta.id);
      applySession("");
      await SetActiveProject(meta.id);
      toast("success", t("toast_project_created", { name: meta.name }));
    } finally {
      setTimeout(() => {
        restoredRef.current = true;
      }, 0);
    }
  };

  const renameProjectFlow = async (id: string, name: string) => {
    const nm = name.trim();
    if (!nm) return;
    await RenameProject(id, nm);
    setProjects(await ListProjects());
    toast("success", t("toast_project_renamed"));
  };

  const deleteProjectFlow = async (id: string) => {
    await DeleteProject(id);
    const list: any = await ListProjects();
    if (!Array.isArray(list) || list.length === 0) {
      const meta: any = await CreateProject(t("default_project_name"));
      setProjects(await ListProjects());
      setActiveId(meta.id);
      applySession("");
      await SetActiveProject(meta.id);
    } else {
      setProjects(list);
      const active: string = await GetActiveProject();
      if (active !== activeIdRef.current) {
        restoredRef.current = false;
        setActiveId(active);
        applySession(await LoadProject(active));
        setTimeout(() => {
          restoredRef.current = true;
        }, 0);
      }
    }
    toast("success", t("toast_project_deleted"));
  };

  // 다이얼로그 오픈 헬퍼
  const openNewProject = () => {
    if (busy) return;
    setNameDialog({ open: true, mode: "new", value: "", targetId: "" });
  };
  const openRenameProject = (id: string) => {
    const p = projects.find((x) => x.id === id);
    setNameDialog({ open: true, mode: "rename", value: p?.name ?? "", targetId: id });
  };
  const submitNameDialog = async () => {
    const { mode, value, targetId } = nameDialog;
    setNameDialog((d) => ({ ...d, open: false }));
    if (mode === "new") await createProjectFlow(value);
    else await renameProjectFlow(targetId, value);
  };
```

- [ ] **Step 6: 상단바 — 드롭다운 + 새 프로젝트 버튼 교체**

`App.tsx:562~565` 의 기존 "새 프로젝트" Button을 아래로 교체:

```tsx
          <ProjectMenu
            projects={projects}
            activeId={activeId}
            busy={busy}
            onSwitch={switchProject}
            onRename={openRenameProject}
            onDelete={(id) => setConfirmDelete({ open: true, id })}
          />
          <Button variant="ghost" size="sm" disabled={busy} onClick={openNewProject} title={t("new_project_tip")}>
            <Plus size={13} /> {t("new_project")}
          </Button>
```

- [ ] **Step 7: 다이얼로그 교체 — confirmNew → 이름 입력 + 삭제 확인**

`App.tsx:647~662` 의 `confirmNew` Dialog 블록을 아래로 교체:

```tsx
      <Dialog open={nameDialog.open} onOpenChange={(o) => !o && setNameDialog((d) => ({ ...d, open: false }))}>
        <DialogContent className="w-[380px]">
          <DialogTitle>{nameDialog.mode === "new" ? t("name_prompt_title_new") : t("name_prompt_title_rename")}</DialogTitle>
          <Input
            autoFocus
            value={nameDialog.value}
            placeholder={t("name_prompt_placeholder")}
            onChange={(e) => setNameDialog((d) => ({ ...d, value: e.target.value }))}
            onKeyDown={(e) => {
              if (e.key === "Enter") submitNameDialog();
            }}
          />
          <div className="row" style={{ justifyContent: "flex-end", gap: 8, marginTop: 4 }}>
            <Button variant="ghost" size="sm" onClick={() => setNameDialog((d) => ({ ...d, open: false }))}>
              {t("cancel")}
            </Button>
            <Button size="sm" onClick={submitNameDialog}>
              {t("name_prompt_ok")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmDelete.open} onOpenChange={(o) => !o && setConfirmDelete({ open: false, id: "" })}>
        <DialogContent className="w-[380px]">
          <DialogTitle>{t("confirm_delete_title")}</DialogTitle>
          <DialogDescription>
            {t("confirm_delete_desc", { name: projects.find((p) => p.id === confirmDelete.id)?.name ?? "" })}
          </DialogDescription>
          <div className="row" style={{ justifyContent: "flex-end", gap: 8, marginTop: 4 }}>
            <Button variant="ghost" size="sm" onClick={() => setConfirmDelete({ open: false, id: "" })}>
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                const id = confirmDelete.id;
                setConfirmDelete({ open: false, id: "" });
                await deleteProjectFlow(id);
              }}
            >
              {t("confirm_delete_ok")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
```

- [ ] **Step 8: 은퇴한 i18n 키 제거**

`frontend/src/i18n/ui.{ko,en,es,zh}.ts` 4개 파일에서 `confirm_new_title`, `confirm_new_desc`, `confirm_new_ok` 세 줄을 각각 삭제한다. (`cancel` 키는 다른 곳에서 쓰이므로 유지. `toast_new_project`는 더 이상 호출되지 않으니 함께 삭제해도 무방.)

- [ ] **Step 9: 타입체크 확인**

Run: `cd ~/StudioProjects/perfectpixel-studio/frontend && npx tsc --noEmit`
Expected: 에러 없음. (`confirmNew`/`handleNewProject`/`resetProject`/`LoadSession`/`SaveSession`/`ClearSession` 잔존 참조가 있으면 모두 제거.)

- [ ] **Step 10: 수동 검증 (`wails dev`)**

> ⚠️ **검증 전 안전 조치:** 실사용 데이터가 들어있는 `~/Library/Application Support/perfectpixel/`를 백업한다.
> `cp -R ~/Library/Application\ Support/perfectpixel ~/Library/Application\ Support/perfectpixel.bak`

Run: `cd ~/StudioProjects/perfectpixel-studio && go run github.com/wailsapp/wails/v2/cmd/wails dev`

체크리스트(각 항목 통과 확인):
1. 첫 실행 시 상단바에 **apple-infantry**가 활성으로 표시되고 7상태가 그대로 복원된다(이관 성공).
2. `+ 새 프로젝트` → 이름 `banana-archer` 입력 → 빈 프로젝트로 전환되고, 드롭다운에 apple-infantry가 **그대로 남아 있다**.
3. 드롭다운으로 apple ↔ banana 전환 시 각자 캐릭터/상태가 **온전**하다(교차 오염 없음).
4. 드롭다운 `이름변경`으로 이름이 바뀌고 표시·목록에 반영된다.
5. 드롭다운 `삭제` → 확인 → 목록에서 사라진다. 활성 프로젝트를 지우면 남은 프로젝트로 전환된다. 마지막 1개까지 지우면 빈 "프로젝트 1"이 자동 생성된다.
6. 앱 종료 후 재실행 시 마지막 활성 프로젝트로 복귀한다.
7. 디스크 확인: `ls ~/Library/Application\ Support/perfectpixel/projects/` 에 `index.json` + `<id>.json`들이 있고, `session.json.migrated`가 보존돼 있다.

- [ ] **Step 11: 커밋**

```bash
cd ~/StudioProjects/perfectpixel-studio
git add frontend/src/App.tsx frontend/src/i18n
git commit -m "feat(projects): App 통합 — 프로젝트 전환·생성·이름변경·삭제

세션 API를 프로젝트 API로 교체, 상단바 드롭다운+새프로젝트,
이름 입력/삭제 확인 다이얼로그, confirmNew 경고 제거."
```

---

## 부록: 실행 후 정리

- 모든 태스크 완료 후 `docs/specs/2026-06-27-project-list-design.md` 의 상태를 "구현 완료"로 갱신(선택).
- `fruit_war_flutter` 쪽 `docs/specs/sprite/STATUS_perfectpixel_apple_2026-06-18.md` § 5 "다음 단계"에 "프로젝트 목록 기능 추가됨 — 유닛별 프로젝트 분리 가능"을 1줄 메모(선택, 별도 레포라 이 계획 범위 밖).
