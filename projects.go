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
