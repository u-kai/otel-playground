package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type Post struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type PostService struct {
	db                *sql.DB
	requestCounter    metric.Int64Counter
	responseTime      metric.Float64Histogram
	activeConnections metric.Int64UpDownCounter
}

func initTracer() (*trace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("post-service"),
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
			semconv.ServiceNameKey.String("post-service"),
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

func initServiceMetrics() (*PostService, error) {
	meter := otel.Meter("post-service")

	requestCounter, err := meter.Int64Counter(
		"post_service_requests_total",
		metric.WithDescription("Total number of requests to post service"),
	)
	if err != nil {
		return nil, err
	}

	responseTime, err := meter.Float64Histogram(
		"post_service_request_duration_seconds",
		metric.WithDescription("Duration of requests to post service"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	activeConnections, err := meter.Int64UpDownCounter(
		"post_service_active_connections",
		metric.WithDescription("Number of active connections to post service"),
	)
	if err != nil {
		return nil, err
	}

	return &PostService{
		requestCounter:    requestCounter,
		responseTime:      responseTime,
		activeConnections: activeConnections,
	}, nil
}

func initDB() (*sql.DB, error) {
	db, err := otelsql.Open("postgres", "host=localhost port=5432 user=postgres password=otelpass dbname=oteldb sslmode=disable")
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

// エラーをスパンに記録するヘルパー関数
func recordError(span oteltrace.Span, err error, description string) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, description)
	}
}

func (s *PostService) getPost(ctx context.Context, postID int) (*Post, error) {
	// SQL操作は otelsql で自動計装されるため、手動スパン不要
	query := "SELECT id, user_id, title, content, created_at FROM posts WHERE id = $1"
	row := s.db.QueryRowContext(ctx, query, postID)

	var post Post
	if err := row.Scan(&post.ID, &post.UserID, &post.Title, &post.Content, &post.CreatedAt); err != nil {
		// 現在のスパンがあればエラーを記録
		if span := oteltrace.SpanFromContext(ctx); span.IsRecording() {
			recordError(span, err, "Failed to scan post data")
		}
		return nil, err
	}

	return &post, nil
}

func (s *PostService) getUserPosts(ctx context.Context, userID int) ([]Post, error) {
	// SQL操作は otelsql で自動計装されるため、手動スパン不要
	query := `
		SELECT id, user_id, title, content, created_at 
		FROM posts 
		WHERE user_id = $1 
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.UserID, &post.Title, &post.Content, &post.CreatedAt); err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	return posts, nil
}

func (s *PostService) getPostHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// アクティブ接続数を増加
	s.activeConnections.Add(r.Context(), 1)
	defer s.activeConnections.Add(r.Context(), -1)

	// トレースコンテキストをヘッダーから抽出
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	
	// ヘッダーにトレースコンテキストが存在するかチェック
	spanCtx := oteltrace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		// 新しいトレースを開始（親トレースが存在しない場合）
		tracer := otel.Tracer("post-service")
		var span oteltrace.Span
		ctx, span = tracer.Start(ctx, "getPostHandler")
		defer span.End()
	}

	// リクエスト処理の最後にメトリクスを記録
	defer func() {
		duration := time.Since(startTime).Seconds()
		s.requestCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/posts"),
		))
		s.responseTime.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/posts"),
		))
	}()

	// 投稿IDをクエリパラメータから取得
	postIDStr := r.URL.Query().Get("id")
	if postIDStr == "" {
		http.Error(w, "post id is required", http.StatusBadRequest)
		return
	}

	postID, err := strconv.Atoi(postIDStr)
	if err != nil {
		http.Error(w, "invalid post id", http.StatusBadRequest)
		return
	}

	// 投稿情報を取得
	post, err := s.getPost(ctx, postID)
	if err != nil {
		// エラーをスパンに記録
		if span := oteltrace.SpanFromContext(ctx); span.IsRecording() {
			if err == sql.ErrNoRows {
				recordError(span, err, "Post not found")
			} else {
				recordError(span, err, "Failed to get post")
			}
		}
		
		if err == sql.ErrNoRows {
			http.Error(w, "post not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// レスポンスヘッダーにトレース情報を注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(w.Header()))
	
	// JSONレスポンスを返す
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(post); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *PostService) getUserPostsHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// アクティブ接続数を増加
	s.activeConnections.Add(r.Context(), 1)
	defer s.activeConnections.Add(r.Context(), -1)

	// トレースコンテキストをヘッダーから抽出
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	
	// ヘッダーにトレースコンテキストが存在するかチェック
	spanCtx := oteltrace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		// 新しいトレースを開始（親トレースが存在しない場合）
		tracer := otel.Tracer("post-service")
		var span oteltrace.Span
		ctx, span = tracer.Start(ctx, "getUserPostsHandler")
		defer span.End()
	}

	// リクエスト処理の最後にメトリクスを記録
	defer func() {
		duration := time.Since(startTime).Seconds()
		s.requestCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/posts/by-user"),
		))
		s.responseTime.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/posts/by-user"),
		))
	}()

	// ユーザーIDをクエリパラメータから取得
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest)
		return
	}

	// ユーザーの投稿一覧を取得
	posts, err := s.getUserPosts(ctx, userID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// レスポンスヘッダーにトレース情報を注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(w.Header()))
	
	// JSONレスポンスを返す
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(posts); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *PostService) healthHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// アクティブ接続数を増加
	s.activeConnections.Add(r.Context(), 1)
	defer s.activeConnections.Add(r.Context(), -1)

	// ヘルスチェックのメトリクスを記録
	defer func() {
		duration := time.Since(startTime).Seconds()
		s.requestCounter.Add(r.Context(), 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/health"),
		))
		s.responseTime.Record(r.Context(), duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/health"),
		))
	}()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "post-service",
	})
}

func (s *PostService) errorHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// アクティブ接続数を増加
	s.activeConnections.Add(r.Context(), 1)
	defer s.activeConnections.Add(r.Context(), -1)

	// エラー検証用エンドポイント
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	// エラーメトリクスを記録
	defer func() {
		duration := time.Since(startTime).Seconds()
		s.requestCounter.Add(ctx, 1, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/error"),
			semconv.HTTPResponseStatusCodeKey.Int(503),
		))
		s.responseTime.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/error"),
		))
	}()
	
	// 意図的にエラーを発生させる
	err := fmt.Errorf("intentional database connection error")
	
	// エラーをスパンに記録
	if span := oteltrace.SpanFromContext(ctx); span.IsRecording() {
		recordError(span, err, "Database connection failed")
		span.SetAttributes(
			semconv.HTTPResponseStatusCodeKey.Int(503),
			semconv.ErrorTypeKey.String("database_error"),
		)
	}
	
	http.Error(w, "Database temporarily unavailable", http.StatusServiceUnavailable)
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

	db, err := initDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	service, err := initServiceMetrics()
	if err != nil {
		log.Fatal(err)
	}
	service.db = db

	mux := http.NewServeMux()
	mux.HandleFunc("/posts", service.getPostHandler)
	mux.HandleFunc("/posts/by-user", service.getUserPostsHandler)
	mux.HandleFunc("/health", service.healthHandler)
	mux.HandleFunc("/error", service.errorHandler)

	// HTTP計装でラップ
	handler := otelhttp.NewHandler(mux, "post-service")

	fmt.Println("🚀 Post service starting on :8081")
	fmt.Println("📊 Endpoints:")
	fmt.Println("  GET /posts?id=1 - Get post by ID")
	fmt.Println("  GET /posts/by-user?user_id=1 - Get posts by user ID")
	fmt.Println("  GET /health - Health check")
	fmt.Println("  GET /error - Test error endpoint")
	fmt.Println("📈 Traces sent to Jaeger: http://localhost:16686")
	fmt.Println("📊 Metrics exported to OTLP: http://localhost:4318")

	if err := http.ListenAndServe(":8081", handler); err != nil {
		log.Fatal(err)
	}
}