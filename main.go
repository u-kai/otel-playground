package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
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
	httpClient       *http.Client
	userBaseURL      string
	postBaseURL      string
	operationCounter metric.Int64Counter
	operationTime    metric.Float64Histogram
	errorCounter     metric.Int64Counter
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

func initMetrics() (*sdkmetric.MeterProvider, error) {
	exporter, err := otlpmetrichttp.New(context.Background())
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

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(5*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return mp, nil
}

func newMicroserviceClient() (*MicroserviceClient, error) {
	// HTTP クライアントにOTEL計装を追加
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	meter := otel.Meter("orchestrator")

	operationCounter, err := meter.Int64Counter(
		"orchestrator_operations_total",
		metric.WithDescription("Total number of operations performed by orchestrator"),
	)
	if err != nil {
		return nil, err
	}

	operationTime, err := meter.Float64Histogram(
		"orchestrator_operation_duration_seconds",
		metric.WithDescription("Duration of operations performed by orchestrator"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	errorCounter, err := meter.Int64Counter(
		"orchestrator_errors_total",
		metric.WithDescription("Total number of errors in orchestrator operations"),
	)
	if err != nil {
		return nil, err
	}

	return &MicroserviceClient{
		httpClient:       httpClient,
		userBaseURL:      "http://localhost:8080",
		postBaseURL:      "http://localhost:8081",
		operationCounter: operationCounter,
		operationTime:    operationTime,
		errorCounter:     errorCounter,
	}, nil
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
	startTime := time.Now()
	
	defer func() {
		duration := time.Since(startTime).Seconds()
		c.operationCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("user-service"),
		))
		c.operationTime.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("user-service"),
		))
	}()

	// HTTP通信は自動計装されるため、手動スパン不要
	url := fmt.Sprintf("%s/users?id=%d", c.userBaseURL, userID)
	body, err := c.callService(ctx, url)
	if err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("user-service"),
		))
		return nil, err
	}

	var user User
	if err := json.Unmarshal(body, &user); err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("user-service"),
		))
		return nil, err
	}

	return &user, nil
}

func (c *MicroserviceClient) getUserPosts(ctx context.Context, userID int) ([]Post, error) {
	startTime := time.Now()
	
	defer func() {
		duration := time.Since(startTime).Seconds()
		c.operationCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("post-service"),
		))
		c.operationTime.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("post-service"),
		))
	}()

	// HTTP通信は自動計装されるため、手動スパン不要
	url := fmt.Sprintf("%s/posts/by-user?user_id=%d", c.postBaseURL, userID)
	body, err := c.callService(ctx, url)
	if err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("post-service"),
		))
		return nil, err
	}

	var posts []Post
	if err := json.Unmarshal(body, &posts); err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("post-service"),
		))
		return nil, err
	}

	return posts, nil
}

func (c *MicroserviceClient) getExternalPost(ctx context.Context, postID int) (*ExternalPost, error) {
	startTime := time.Now()
	
	defer func() {
		duration := time.Since(startTime).Seconds()
		c.operationCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("external-api"),
		))
		c.operationTime.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("external-api"),
		))
	}()

	// HTTP通信は自動計装されるため、手動スパン不要
	url := fmt.Sprintf("https://jsonplaceholder.typicode.com/posts/%d", postID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("external-api"),
		))
		return nil, err
	}

	// 外部APIにもトレースコンテキストを注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("external-api"),
		))
		return nil, err
	}
	defer resp.Body.Close()

	var post ExternalPost
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServiceNameKey.String("external-api"),
		))
		return nil, err
	}

	return &post, nil
}

// ObservabilityCheck checks if observability tools are working
func checkObservabilityTools(ctx context.Context, client *MicroserviceClient) {
	fmt.Println("\n🔍 Checking Observability Tools Status...")
	
	// Check OTEL Collector health
	fmt.Printf("📊 OTEL Collector: ")
	if err := checkHTTPEndpoint("http://localhost:8889/metrics"); err != nil {
		fmt.Printf("❌ Not accessible (%v)\n", err)
	} else {
		fmt.Printf("✅ Running (metrics endpoint accessible)\n")
	}

	// Check Prometheus
	fmt.Printf("📈 Prometheus: ")
	if err := checkHTTPEndpoint("http://localhost:9090/-/healthy"); err != nil {
		fmt.Printf("❌ Not accessible (%v)\n", err)
	} else {
		fmt.Printf("✅ Running\n")
	}

	// Check if Prometheus is scraping OTEL Collector metrics
	fmt.Printf("🔗 Prometheus ← OTEL Collector: ")
	if metrics, err := checkPrometheusMetrics(); err != nil {
		fmt.Printf("❌ Metrics not found (%v)\n", err)
	} else {
		fmt.Printf("✅ %d OTEL metrics found\n", metrics)
	}

	// Check Jaeger
	fmt.Printf("🔍 Jaeger: ")
	if err := checkHTTPEndpoint("http://localhost:16686/search"); err != nil {
		fmt.Printf("❌ Not accessible (%v)\n", err)
	} else {
		fmt.Printf("✅ Running\n")
	}

	// Check if Jaeger has received traces
	fmt.Printf("🔗 Jaeger ← OTEL Collector: ")
	if traces, err := checkJaegerTraces(); err != nil {
		fmt.Printf("❌ Traces not found (%v)\n", err)
	} else {
		fmt.Printf("✅ %d services found with traces\n", traces)
	}

	fmt.Println()
}

func checkHTTPEndpoint(url string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func checkPrometheusMetrics() (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://localhost:9090/api/v1/label/__name__/values")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	// Count OTEL-related metrics
	bodyStr := string(body)
	otelMetrics := strings.Count(bodyStr, "otel_")
	
	return otelMetrics, nil
}

func checkJaegerTraces() (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://localhost:16686/api/services")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []string `json:"data"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return len(result.Data), nil
}

// 🎯 ViewとExemplarのデモンストレーション
func demonstrateViewsAndExemplars(ctx context.Context, client *MicroserviceClient) {
	fmt.Println("📊 Creating different performance scenarios for Views and Exemplars...")
	
	// シナリオ1: 通常のリクエスト
	fmt.Printf("1️⃣ Normal requests (fast)...")
	for i := 1; i <= 3; i++ {
		_, err := client.getUser(ctx, i)
		if err != nil {
			fmt.Printf(" Error: %v", err)
		}
		fmt.Printf(".")
	}
	fmt.Println(" ✅")
	
	// シナリオ2: 中程度の遅延
	fmt.Printf("2️⃣ Medium latency requests...")
	for i := 100; i <= 102; i++ {
		_, err := client.getUser(ctx, i)
		if err != nil {
			fmt.Printf(" Error: %v", err)
		}
		fmt.Printf(".")
	}
	fmt.Println(" ✅")
	
	// シナリオ3: 高遅延リクエスト（Exemplarで特定できる）
	fmt.Printf("3️⃣ High latency request (will create exemplar)...")
	_, err := client.getUser(ctx, 999)
	if err != nil {
		fmt.Printf(" Error: %v", err)
	}
	fmt.Println(" ✅")
	
	// シナリオ4: エラーリクエスト
	fmt.Printf("4️⃣ Error requests (for error rate view)...")
	for i := 9990; i <= 9992; i++ {
		_, err := client.getUser(ctx, i)
		if err != nil {
			fmt.Printf(".")
		}
	}
	fmt.Println(" ✅")
	
	fmt.Println("✨ Views & Exemplars demonstration completed!")
	fmt.Println("   - Different latency buckets will show in custom histograms")
	fmt.Println("   - High latency requests will have exemplar links to traces")
	fmt.Println("   - Error rates will be aggregated in error_rate view")
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

	mp, err := initMetrics()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down meter provider: %v", err)
		}
	}()

	// マイクロサービスクライアントを初期化
	client, err := newMicroserviceClient()
	if err != nil {
		log.Fatal(err)
	}

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

	// Wait a bit for services to start up and register metrics/traces
	fmt.Println("⏳ Waiting 3 seconds for observability tools to collect initial data...")
	time.Sleep(3 * time.Second)

	// Check observability tools status before starting
	checkObservabilityTools(ctx, client)

	userID := 1
	if err := orchestrateUserData(ctx, client, userID); err != nil {
		log.Fatalf("Orchestration failed: %v", err)
	}

	fmt.Println("\n✅ Orchestration completed successfully!")
	
	// 🎯 ViewとExemplarのデモ
	fmt.Println("\n🎯 Demonstrating Views and Exemplars...")
	demonstrateViewsAndExemplars(ctx, client)
	
	// Wait for metrics and traces to be exported
	fmt.Println("⏳ Waiting 5 seconds for metrics and traces to be exported...")
	time.Sleep(5 * time.Second)

	// Check observability tools status after operations
	fmt.Println("🔍 Post-operation observability check:")
	checkObservabilityTools(ctx, client)

	fmt.Println("📈 Access URLs:")
	fmt.Println("  - Jaeger UI: http://localhost:16686")
	fmt.Println("  - Prometheus UI: http://localhost:9090")
	fmt.Println("  - OTEL Collector metrics: http://localhost:8889/metrics")
	fmt.Println("🔍 Look for 'orchestrator', 'user-service', 'post-service' in Jaeger/Prometheus")
	
	fmt.Println("\n🎓 View & Exemplar Learning:")
	fmt.Println("  1. Check Prometheus UI for 'user_service_response_time_custom' (View)")
	fmt.Println("  2. Look for 'user_service_error_rate' (View)")
	fmt.Println("  3. Click on exemplar links in histograms to jump to traces (Exemplar)")
	fmt.Println("  4. Notice custom bucket boundaries in the histogram")
}