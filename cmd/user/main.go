package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	_ "github.com/lib/pq"
	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
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
	db *sql.DB
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
	// ヘルスチェックは軽量なので手動スパン不要
	// otelhttp.NewHandler で自動計装される

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "user-service",
	})
}

func (s *UserService) errorHandler(w http.ResponseWriter, r *http.Request) {
	// エラー検証用エンドポイント
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

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

	db, err := initDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	service := &UserService{
		db: db,
	}

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

	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

