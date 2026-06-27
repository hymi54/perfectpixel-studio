package main

import (
	"os"
	"path/filepath"
	"testing"

	"perfectpixel/internal/config"
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

	// ListProjects는 updatedAt 내림차순이어야 함 (동률 허용)
	ordered := a.ListProjects()
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].UpdatedAt < ordered[i].UpdatedAt {
			t.Fatalf("ListProjects가 updatedAt 내림차순이 아님: %+v", ordered)
		}
	}

	// SetActiveProject
	if err := a.SetActiveProject(meta.ID); err != nil {
		t.Fatalf("SetActive 실패: %v", err)
	}
	if a.GetActiveProject() != meta.ID {
		t.Fatalf("활성 전환 실패")
	}
}

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
