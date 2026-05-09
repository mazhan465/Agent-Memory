package searcher

import (
	"context"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

type fakeEmbedder struct{}

func (f fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{1, 0}, nil
}

func (f fakeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for range texts {
		vectors = append(vectors, []float32{1, 0})
	}
	return vectors, nil
}

func (f fakeEmbedder) Dimension() int { return 2 }

func (f fakeEmbedder) Provider() string { return "fake" }

type fakeVectorStore struct {
	options vectorstore.SearchOptions
}

func (f *fakeVectorStore) Put(ctx context.Context, namespace string, documents []vectorstore.Document) error {
	return nil
}

func (f *fakeVectorStore) ReplaceFiles(
	ctx context.Context,
	namespace string,
	relativePaths []string,
	documents []vectorstore.Document,
) error {
	return nil
}

func (f *fakeVectorStore) Search(ctx context.Context, namespace string, queryVector []float32, options vectorstore.SearchOptions) ([]vectorstore.SearchResult, error) {
	f.options = options
	return nil, nil
}

func (f *fakeVectorStore) Clear(ctx context.Context, namespace string) error { return nil }

func (f *fakeVectorStore) Count(ctx context.Context, namespace string) (int, error) { return 0, nil }

func TestSearchAppliesStrongDomainFilters(t *testing.T) {
	store := &fakeVectorStore{}
	searcher := New(fakeEmbedder{}, store)
	_, err := searcher.Search(context.Background(), ".", "milvus collection vector store", vectorstore.SearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []string{"database/milvus", "database"}
	if len(store.options.DomainFilters) != len(want) {
		t.Fatalf("DomainFilters = %+v, want %+v", store.options.DomainFilters, want)
	}
	for index, value := range want {
		if store.options.DomainFilters[index] != value {
			t.Fatalf("DomainFilters[%d] = %s, want %s", index, store.options.DomainFilters[index], value)
		}
	}
}
