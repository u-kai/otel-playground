package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/uptrace/opentelemetry-go-extra/otelsql"
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Post struct {
	UserID int    `json:"userId"`
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type DBPost struct {
	ID        int    `db:"id"`
	UserID    int    `db:"user_id"`
	Title     string `db:"title"`
	Content   string `db:"content"`
	CreatedAt string `db:"created_at"`
}

type User struct {
	ID        int    `db:"id"`
	Name      string `db:"name"`
	Email     string `db:"email"`
	CreatedAt string `db:"created_at"`
}

func initTracer() (*trace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("otel-playground"),
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

func getUserPosts(ctx context.Context, db *sql.DB, userID int) ([]DBPost, error) {
	tracer := otel.Tracer("otel-playground")
	ctx, span := tracer.Start(ctx, "getUserPosts")
	defer span.End()

	query := `
		SELECT id, user_id, title, content, created_at 
		FROM posts 
		WHERE user_id = $1 
		ORDER BY created_at DESC
	`

	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []DBPost
	for rows.Next() {
		var post DBPost
		if err := rows.Scan(&post.ID, &post.UserID, &post.Title, &post.Content, &post.CreatedAt); err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	return posts, nil
}

func getUser(ctx context.Context, db *sql.DB, userID int) (*User, error) {
	tracer := otel.Tracer("otel-playground")
	ctx, span := tracer.Start(ctx, "getUser")
	defer span.End()

	query := "SELECT id, name, email, created_at FROM users WHERE id = $1"
	row := db.QueryRowContext(ctx, query, userID)

	var user User
	if err := row.Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt); err != nil {
		return nil, err
	}

	return &user, nil
}

func fetchPost(ctx context.Context, postID int) (*Post, error) {
	tracer := otel.Tracer("otel-playground")
	ctx, span := tracer.Start(ctx, "fetchPost")
	defer span.End()

	url := fmt.Sprintf("https://jsonplaceholder.typicode.com/posts/%d", postID)
	
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var post Post
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		return nil, err
	}

	return &post, nil
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

	ctx := context.Background()
	
	// HTTP APIからデータを取得
	post, err := fetchPost(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("=== API Post ===\n")
	fmt.Printf("Post ID: %d\n", post.ID)
	fmt.Printf("Title: %s\n", post.Title)
	fmt.Printf("Body: %s\n", post.Body)

	// データベースからユーザー情報を取得
	userID := 1
	user, err := getUser(ctx, db, userID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n=== Database User ===\n")
	fmt.Printf("User ID: %d\n", user.ID)
	fmt.Printf("Name: %s\n", user.Name)
	fmt.Printf("Email: %s\n", user.Email)

	// データベースからユーザーの投稿を取得
	posts, err := getUserPosts(ctx, db, userID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n=== Database Posts ===\n")
	for i, dbPost := range posts {
		if i >= 3 { // 最初の3件のみ表示
			break
		}
		fmt.Printf("Post %d: %s\n", dbPost.ID, dbPost.Title)
	}

	fmt.Println("\nTraces sent to Jaeger! Check http://localhost:16686")
}