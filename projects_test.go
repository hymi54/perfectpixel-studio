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
