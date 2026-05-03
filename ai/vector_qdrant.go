package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"seckill/utils"
)

// QdrantStore Qdrant 向量存储实现（REST API）
type QdrantStore struct {
	addr       string
	collection string
	vecSize    int
	httpClient *http.Client
}

// NewQdrantStore 创建 Qdrant Store
func NewQdrantStore(addr string, collection string, vecSize int) (*QdrantStore, error) {
	store := &QdrantStore{
		addr:       addr,
		collection: collection,
		vecSize:    vecSize,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	// 初始化 collection
	if err := store.initCollection(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *QdrantStore) initCollection() error {
	// 检查 collection 是否存在
	exists, _ := s.collectionExists()
	if exists {
		return nil
	}

	// 创建 collection
	url := fmt.Sprintf("http://%s/collections/%s", s.addr, s.collection)
	body := map[string]any{
		"vectors": map[string]any{
			"size":     s.vecSize,
			"distance": "Cosine",
		},
	}

	jsonData, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create collection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection failed: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (s *QdrantStore) collectionExists() (bool, error) {
	url := fmt.Sprintf("http://%s/collections/%s", s.addr, s.collection)
	req, _ := http.NewRequest(http.MethodGet, url, nil)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

func (s *QdrantStore) Upsert(chunks []Chunk, vectors [][]float32) error {
	url := fmt.Sprintf("http://%s/collections/%s/points", s.addr, s.collection)

	points := make([]map[string]any, len(chunks))
	for i := range chunks {
		// Qdrant 要求 ID 必须是整数或 UUID，用索引作为整数 ID
		points[i] = map[string]any{
			"id":      i + 1,  // 用索引作为整数 ID
			"vector":  vectors[i],
			"payload": map[string]any{
				"content": chunks[i].Content,
				"chunk_id": chunks[i].ID,
			},
		}
	}

	body := map[string]any{"points": points}
	jsonData, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upsert points failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert points failed: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (s *QdrantStore) Search(queryVector []float32, topK int) ([]RetrievedChunk, error) {
	utils.Info("qdrant_search",
		utils.Int("query_vec_dim", len(queryVector)),
		utils.Int("topK", topK),
	)
	url := fmt.Sprintf("http://%s/collections/%s/points/search", s.addr, s.collection)

	body := map[string]any{
		"vector": queryVector,
		"top":    topK,
		"with_payload": map[string]any{"include": []string{"content"}},
	}
	jsonData, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result []struct {
			ID      interface{}            `json:"id"`
			Score   float32                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response failed: %w", err)
	}

	chunks := make([]RetrievedChunk, 0, len(result.Result))
	for _, r := range result.Result {
		if content, ok := r.Payload["content"].(string); ok {
			// ID 可能是数字或字符串
			var idStr string
			switch v := r.ID.(type) {
			case string:
				idStr = v
			case float64:
				idStr = fmt.Sprintf("%d", int(v))
			default:
				idStr = fmt.Sprintf("%v", v)
			}
			// 优先使用 chunk_id
			if cid, ok := r.Payload["chunk_id"].(string); ok {
				idStr = cid
			}
			chunks = append(chunks, RetrievedChunk{
				ID:      idStr,
				Content: content,
				Score:   r.Score,
			})
		}
	}

	return chunks, nil
}

func (s *QdrantStore) DeleteAll() error {
	url := fmt.Sprintf("http://%s/collections/%s/points/delete", s.addr, s.collection)

	body := map[string]any{"filter": map[string]any{}}
	jsonData, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete all failed: %w", err)
	}
	defer resp.Body.Close()

	return nil
}
