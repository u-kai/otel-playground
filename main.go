package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Post struct {
	UserID int    `json:"userId"`
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func initTracer() (*trace.TracerProvider, error) {
	exporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)

	return tp, nil
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

	ctx := context.Background()
	post, err := fetchPost(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Post ID: %d\n", post.ID)
	fmt.Printf("Title: %s\n", post.Title)
	fmt.Printf("Body: %s\n", post.Body)

	fmt.Fprintln(os.Stderr, "\n--- Trace output above ---")
}