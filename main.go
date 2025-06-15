package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// API response types
type User struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

type Post struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// External API types (JSONPlaceholder)
type ExternalPost struct {
	UserID int    `json:"userId"`
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type MicroserviceClient struct {
	httpClient   *http.Client
	userBaseURL  string
	postBaseURL  string
}

func initTracer() (*trace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("orchestrator"),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	
	// トレースコンテキストの伝播設定
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp, nil
}

func newMicroserviceClient() *MicroserviceClient {
	// HTTP クライアントにOTEL計装を追加
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	return &MicroserviceClient{
		httpClient:  client,
		userBaseURL: "http://localhost:8080",
		postBaseURL: "http://localhost:8081",
	}
}

func (c *MicroserviceClient) callService(ctx context.Context, url string) ([]byte, error) {
	// HTTP クライアントは otelhttp.NewTransport で自動計装されるため、手動スパン不要

	// HTTP リクエストを作成
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// トレースコンテキストをリクエストヘッダーに注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// HTTP リクエストを実行
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service returned status: %d", resp.StatusCode)
	}

	// レスポンスボディを読み取り
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (c *MicroserviceClient) callServiceIgnoreError(ctx context.Context, url string) ([]byte, error) {
	// エラーテスト用 - HTTPエラーステータスでもエラーとして扱わない
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// トレースコンテキストをリクエストヘッダーに注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// HTTP リクエストを実行
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// エラーステータスでもボディを読み取る
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// HTTPエラーステータスの場合はエラーとして返す
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *MicroserviceClient) getUser(ctx context.Context, userID int) (*User, error) {
	// HTTP通信は自動計装されるため、手動スパン不要
	url := fmt.Sprintf("%s/users?id=%d", c.userBaseURL, userID)
	body, err := c.callService(ctx, url)
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (c *MicroserviceClient) getUserPosts(ctx context.Context, userID int) ([]Post, error) {
	// HTTP通信は自動計装されるため、手動スパン不要
	url := fmt.Sprintf("%s/posts/by-user?user_id=%d", c.postBaseURL, userID)
	body, err := c.callService(ctx, url)
	if err != nil {
		return nil, err
	}

	var posts []Post
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, err
	}

	return posts, nil
}

func (c *MicroserviceClient) getExternalPost(ctx context.Context, postID int) (*ExternalPost, error) {
	// HTTP通信は自動計装されるため、手動スパン不要
	url := fmt.Sprintf("https://jsonplaceholder.typicode.com/posts/%d", postID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 外部APIにもトレースコンテキストを注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var post ExternalPost
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		return nil, err
	}

	return &post, nil
}

func orchestrateUserData(ctx context.Context, client *MicroserviceClient, userID int) error {
	// 複数サービスの統合処理なので、ビジネスロジック用のスパンを作成
	tracer := otel.Tracer("orchestrator")
	ctx, span := tracer.Start(ctx, "orchestrateUserData")
	defer span.End()

	// 1. ユーザー情報を取得（user-service経由）
	user, err := client.getUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	fmt.Printf("=== User Information ===\n")
	fmt.Printf("User ID: %d\n", user.ID)
	fmt.Printf("Name: %s\n", user.Name)
	fmt.Printf("Email: %s\n", user.Email)

	// 2. ユーザーの投稿を取得（post-service経由）
	posts, err := client.getUserPosts(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user posts: %w", err)
	}

	fmt.Printf("\n=== User Posts (from post-service) ===\n")
	for i, post := range posts {
		if i >= 3 { // 最初の3件のみ表示
			break
		}
		fmt.Printf("Post %d: %s\n", post.ID, post.Title)
	}

	// 3. 外部APIから投稿を取得（比較用）
	externalPost, err := client.getExternalPost(ctx, 1)
	if err != nil {
		return fmt.Errorf("failed to get external post: %w", err)
	}

	fmt.Printf("\n=== External Post (JSONPlaceholder) ===\n")
	fmt.Printf("Post ID: %d\n", externalPost.ID)
	fmt.Printf("Title: %s\n", externalPost.Title)
	fmt.Printf("Body: %s\n", externalPost.Body)

	// 4. エラーエンドポイントを呼び出してエラートレーシングをテスト
	fmt.Printf("\n=== Testing Error Tracing ===\n")
	
	// user-serviceのエラーエンドポイント
	fmt.Printf("Testing user-service error endpoint...\n")
	_, err = client.callServiceIgnoreError(ctx, fmt.Sprintf("%s/error", client.userBaseURL))
	if err != nil {
		fmt.Printf("✅ Expected error from user-service: %v\n", err)
	}
	
	// post-serviceのエラーエンドポイント
	fmt.Printf("Testing post-service error endpoint...\n")
	_, err = client.callServiceIgnoreError(ctx, fmt.Sprintf("%s/error", client.postBaseURL))
	if err != nil {
		fmt.Printf("✅ Expected error from post-service: %v\n", err)
	}

	return nil
}

func main() {
	tp, err := initTracer()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	// マイクロサービスクライアントを初期化
	client := newMicroserviceClient()

	// メインのオーケストレーション処理を開始
	tracer := otel.Tracer("orchestrator")
	ctx, mainSpan := tracer.Start(context.Background(), "main_orchestration")
	defer mainSpan.End()

	fmt.Println("🚀 Starting microservice orchestration...")
	fmt.Println("📊 This will call:")
	fmt.Println("  - user-service (localhost:8080)")
	fmt.Println("  - post-service (localhost:8081)")
	fmt.Println("  - JSONPlaceholder API (external)")
	fmt.Println()

	userID := 1
	if err := orchestrateUserData(ctx, client, userID); err != nil {
		log.Fatalf("Orchestration failed: %v", err)
	}

	fmt.Println("\n✅ Orchestration completed successfully!")
	fmt.Println("📈 End-to-end traces available at: http://localhost:16686")
	fmt.Println("🔍 Look for 'orchestrator' service in Jaeger UI")
}