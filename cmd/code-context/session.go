// 文件说明：提供搜索会话级去重状态管理。
// 实现原理：按 session_id 将已返回的 result key 和 content hash 持久化到本地 JSON 文件，后续搜索返回前过滤已出现结果。
// 使用方式：searchAll 在返回结果前调用 loadSearchSession、filterSessionResults 和 saveSearchSession。
// 注意事项：会话去重只影响搜索返回，不修改向量库中的原始数据。
// 交互模块：cmd/code-context/search.go、internal/vectorstore。

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	searchSessionDirName = "sessions"
	sessionIDRandomBytes = 16
)

var validSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type searchSessionState struct {
	ID                    string    `json:"id"`
	ReturnedResultKeys    []string  `json:"returned_result_keys"`
	ReturnedContentHashes []string  `json:"returned_content_hashes"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type searchSessionStore struct {
	storageDir string
}

func newSearchSessionStore(storageDir string) *searchSessionStore {
	return &searchSessionStore{storageDir: storageDir}
}

func (s *searchSessionStore) LoadOrCreate(sessionID string) (searchSessionState, error) {
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		generatedSessionID, err := generateSessionID()
		if err != nil {
			return searchSessionState{}, err
		}
		return newSearchSessionState(generatedSessionID), nil
	}
	if !validSessionID.MatchString(cleanSessionID) {
		return searchSessionState{}, fmt.Errorf("invalid session id %q", sessionID)
	}
	state, err := s.load(cleanSessionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newSearchSessionState(cleanSessionID), nil
		}
		return searchSessionState{}, err
	}
	return state, nil
}

func (s *searchSessionStore) Save(state searchSessionState) error {
	if !validSessionID.MatchString(state.ID) {
		return fmt.Errorf("invalid session id %q", state.ID)
	}
	if err := os.MkdirAll(s.sessionDir(), 0755); err != nil {
		return err
	}
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tempPath := s.sessionPath(state.ID) + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tempPath, s.sessionPath(state.ID))
}

func (s *searchSessionStore) load(sessionID string) (searchSessionState, error) {
	data, err := os.ReadFile(s.sessionPath(sessionID))
	if err != nil {
		return searchSessionState{}, err
	}
	var state searchSessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return searchSessionState{}, err
	}
	if state.ID == "" {
		state.ID = sessionID
	}
	return state, nil
}

func (s *searchSessionStore) sessionDir() string {
	return filepath.Join(s.storageDir, searchSessionDirName)
}

func (s *searchSessionStore) sessionPath(sessionID string) string {
	return filepath.Join(s.sessionDir(), sessionID+".json")
}

func newSearchSessionState(sessionID string) searchSessionState {
	now := time.Now()
	return searchSessionState{ID: sessionID, CreatedAt: now, UpdatedAt: now}
}

func generateSessionID() (string, error) {
	buffer := make([]byte, sessionIDRandomBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "session_" + hex.EncodeToString(buffer), nil
}

func filterSessionResults(
	state *searchSessionState,
	results []categorizedSearchResult,
	limit int,
) ([]categorizedSearchResult, int) {
	seenResultKeys := makeStringSetFromSlice(state.ReturnedResultKeys)
	seenContentHashes := makeStringSetFromSlice(state.ReturnedContentHashes)
	filtered := make([]categorizedSearchResult, 0, len(results))
	dedupedCount := 0
	for _, result := range results {
		resultKey := searchResultKey(result)
		contentHash := searchContentHash(result.Result.Document.Content)
		if _, ok := seenResultKeys[resultKey]; ok {
			dedupedCount++
			continue
		}
		if _, ok := seenContentHashes[contentHash]; ok {
			dedupedCount++
			continue
		}
		if limit > 0 && len(filtered) >= limit {
			continue
		}
		filtered = append(filtered, result)
		seenResultKeys[resultKey] = struct{}{}
		seenContentHashes[contentHash] = struct{}{}
		state.ReturnedResultKeys = append(state.ReturnedResultKeys, resultKey)
		state.ReturnedContentHashes = append(state.ReturnedContentHashes, contentHash)
	}
	return filtered, dedupedCount
}

func searchResultKey(result categorizedSearchResult) string {
	return strings.Join([]string{result.Namespace, result.Result.Document.ID}, "\x00")
}

func searchContentHash(content string) string {
	hash := sha256.Sum256([]byte(normalizeSearchContent(content)))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func normalizeSearchContent(content string) string {
	return strings.Join(strings.Fields(content), " ")
}

func makeStringSetFromSlice(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}
