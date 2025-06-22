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

	_ "github.com/lib/pq"
	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type User struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

type UserService struct {
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
			semconv.ServiceNameKey.String("user-service"),
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
			semconv.ServiceNameKey.String("user-service"),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// 🎯 研究に基づく正しいViews & Exemplars実装
	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(5*time.Second))
	
	// Custom histogram view with custom buckets
	customHistogramView := sdkmetric.NewView(
		sdkmetric.Instrument{
			Name: "user_service_request_duration_seconds",
		},
		sdkmetric.Stream{
			Name:        "user_service_response_time_custom",
			Description: "Custom histogram with exemplar support",
			Unit:        "s",
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
			},
			// 🔗 Custom exemplar reservoir for histograms
			ExemplarReservoirProviderSelector: func(agg sdkmetric.Aggregation) exemplar.ReservoirProvider {
				if _, ok := agg.(sdkmetric.AggregationExplicitBucketHistogram); ok {
					// Use histogram reservoir for histogram aggregation
					return exemplar.HistogramReservoirProvider([]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0})
				}
				return exemplar.FixedSizeReservoirProvider(10)
			},
		},
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
		sdkmetric.WithView(customHistogramView),
		// 🔗 Enable trace-based exemplar filtering
		sdkmetric.WithExemplarFilter(exemplar.TraceBasedFilter),
	)
	otel.SetMeterProvider(mp)

	return mp, nil
}

func initServiceMetrics() (*UserService, error) {
	meter := otel.Meter("user-service")

	requestCounter, err := meter.Int64Counter(
		"user_service_requests_total",
		metric.WithDescription("Total number of requests to user service"),
	)
	if err != nil {
		return nil, err
	}

	responseTime, err := meter.Float64Histogram(
		"user_service_request_duration_seconds",
		metric.WithDescription("Duration of requests to user service"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	activeConnections, err := meter.Int64UpDownCounter(
		"user_service_active_connections",
		metric.WithDescription("Number of active connections to user service"),
	)
	if err != nil {
		return nil, err
	}

	return &UserService{
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

func (s *UserService) getUser(ctx context.Context, userID int) (*User, error) {
	// SQL操作は otelsql で自動計装されるため、手動スパン不要
	query := "SELECT id, name, email, created_at FROM users WHERE id = $1"
	row := s.db.QueryRowContext(ctx, query, userID)

	var user User
	if err := row.Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt); err != nil {
		// 現在のスパンがあればエラーを記録
		if span := oteltrace.SpanFromContext(ctx); span.IsRecording() {
			recordError(span, err, "Failed to scan user data")
		}
		return nil, err
	}

	return &user, nil
}

func (s *UserService) getUserHandler(w http.ResponseWriter, r *http.Request) {
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
		tracer := otel.Tracer("user-service")
		var span oteltrace.Span
		ctx, span = tracer.Start(ctx, "getUserHandler")
		defer span.End()
	}
	// HTTP操作は otelhttp.NewHandler で自動計装されるため、通常は手動スパン不要

	// リクエスト処理の最後にメトリクスを記録（Exemplar対応）
	defer func() {
		duration := time.Since(startTime).Seconds()
		
		// Exemplar: トレースコンテキストを含むメトリクス記録
		// これによりPrometheusでメトリクスからトレースにジャンプできる
		attrs := metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/users"),
		)
		
		s.requestCounter.Add(ctx, 1, attrs)
		s.responseTime.Record(ctx, duration, attrs)
		
		// 🔍 Debug: Confirm histogram recording
		fmt.Printf("📊 Recorded histogram: duration=%.3fs, method=%s, route=%s\n", 
			duration, r.Method, "/users")
		
		// 特別なメトリクス：時間のかかるリクエストを記録
		if duration > 0.1 { // 100ms以上
			fmt.Printf("🐌 Slow request detected: %.3fs for %s %s\n", 
				duration, r.Method, r.URL.Path)
		}
	}()

	// ユーザーIDをパスパラメータから取得
	userIDStr := r.URL.Query().Get("id")
	if userIDStr == "" {
		http.Error(w, "user id is required", http.StatusBadRequest)
		return
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	// 🎯 デモ用：意図的に遅延を追加（ViewとExemplarの体験用）
	if userID == 999 {
		fmt.Printf("🐌 Simulating slow database query for user %d...\n", userID)
		time.Sleep(2 * time.Second) // 2秒の遅延
	} else if userID >= 100 && userID <= 110 {
		fmt.Printf("⏱️ Medium delay for user %d...\n", userID)
		time.Sleep(200 * time.Millisecond) // 200msの遅延
	}

	// ユーザー情報を取得
	user, err := s.getUser(ctx, userID)
	if err != nil {
		// エラーをスパンに記録
		if span := oteltrace.SpanFromContext(ctx); span.IsRecording() {
			if err == sql.ErrNoRows {
				recordError(span, err, "User not found")
			} else {
				recordError(span, err, "Failed to get user")
			}
		}

		if err == sql.ErrNoRows {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// レスポンスヘッダーにトレース情報を注入
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(w.Header()))

	// JSONレスポンスを返す
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(user); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *UserService) healthHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// アクティブ接続数を増加
	s.activeConnections.Add(r.Context(), 1)
	defer s.activeConnections.Add(r.Context(), -1)

	// ヘルスチェックのメトリクスを記録
	defer func() {
		duration := time.Since(startTime).Seconds()
		attrs := metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/health"),
		)
		s.requestCounter.Add(r.Context(), 1, attrs)
		s.responseTime.Record(r.Context(), duration, attrs)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "user-service",
	})
}

func (s *UserService) errorHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// アクティブ接続数を増加
	s.activeConnections.Add(r.Context(), 1)
	defer s.activeConnections.Add(r.Context(), -1)

	// エラー検証用エンドポイント
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	// エラーメトリクスを記録
	defer func() {
		duration := time.Since(startTime).Seconds()
		attrs := metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String("/error"),
			semconv.HTTPResponseStatusCodeKey.Int(500),
		)
		s.requestCounter.Add(ctx, 1, attrs)
		s.responseTime.Record(ctx, duration, attrs)
	}()

	// 意図的にエラーを発生させる
	err := fmt.Errorf("intentional error for testing")

	// エラーをスパンに記録
	if span := oteltrace.SpanFromContext(ctx); span.IsRecording() {
		recordError(span, err, "Intentional test error")
		span.SetAttributes(
			semconv.HTTPResponseStatusCodeKey.Int(500),
			semconv.ErrorTypeKey.String("test_error"),
		)
	}

	defer otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(w.Header()))

	http.Error(w, "This is a test error endpoint", http.StatusInternalServerError)
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
	mux.HandleFunc("/users", service.getUserHandler)
	mux.HandleFunc("/health", service.healthHandler)
	mux.HandleFunc("/error", service.errorHandler)

	// HTTP計装でラップ
	handler := otelhttp.NewHandler(mux, "user-service")

	fmt.Println("🚀 User service starting on :8080")
	fmt.Println("📊 Endpoints:")
	fmt.Println("  GET /users?id=1 - Get user by ID")
	fmt.Println("  GET /health - Health check")
	fmt.Println("  GET /error - Test error endpoint")
	fmt.Println("📈 Traces sent to Jaeger: http://localhost:16686")
	fmt.Println("📊 Metrics exported to OTLP: http://localhost:4318")

	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

