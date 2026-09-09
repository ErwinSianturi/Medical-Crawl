package crawler

import (
	"context"

	"maps-scraper/pkg/model"
)

// Source represents an abstract content source (e.g. a trusted publication or site).
type Source interface {
	Name() string
	BaseURL() string
}

// TaskDiscoverer is responsible for discovering tasks (e.g. article URLs) from a source.
type TaskDiscoverer interface {
	DiscoverTasks(ctx context.Context, source Source) ([]Task, error)
}

// HTTPFetcher abstracts HTTP network retrieval, enabling clean testing and pluggable transport.
type HTTPFetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

// ArticleExtractor extracts a canonical model.Article from raw fetched content.
type ArticleExtractor interface {
	Extract(ctx context.Context, task Task, content []byte) (*model.Article, error)
}
