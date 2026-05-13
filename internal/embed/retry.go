// 文件说明：提供 embedding 接口调用重试逻辑。
// 实现原理：对临时或服务端接口错误进行有限次数重试，并在最终失败时返回包含尝试次数的错误。
// 使用方式：OpenAI 和 Ollama embedder 在单次 HTTP 批量请求外层调用 retryEmbeddingRequest。
// 注意事项：重试会响应 context 取消，避免索引被无限阻塞。
// 交互模块：internal/embed/openai.go、internal/embed/ollama.go、internal/indexer。

package embed

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultEmbeddingRequestRetries = 3
	defaultEmbeddingRetryDelay     = 100 * time.Millisecond
)

func retryEmbeddingRequest(
	ctx context.Context,
	provider string,
	operation func(context.Context) ([][]float32, error),
) ([][]float32, error) {
	var lastErr error
	maxAttempts := defaultEmbeddingRequestRetries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		vectors, err := operation(ctx)
		if err == nil {
			return vectors, nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		if err := waitEmbeddingRetry(ctx, attempt); err != nil {
			return nil, fmt.Errorf("%s embedding request retry canceled after attempt=%d: %w", provider, attempt, err)
		}
	}
	return nil, fmt.Errorf("%s embedding request failed after %d attempts: %w", provider, maxAttempts, lastErr)
}

func waitEmbeddingRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt) * defaultEmbeddingRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
