package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func main() {
	fmt.Println("🧪 Testing Simple Histogram Export")
	
	// Initialize basic metrics without Views
	exporter, err := otlpmetrichttp.New(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("simple-test"),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(2*time.Second))
	
	// Simple MeterProvider without Views
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	meter := otel.Meter("simple-test")
	
	// Create histogram without Views
	histogram, err := meter.Float64Histogram(
		"simple_test_duration_seconds",
		metric.WithDescription("Simple test duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Println("✅ Histogram created successfully")
	
	// Record some values
	ctx := context.Background()
	
	fmt.Println("📊 Recording histogram values...")
	for i := 0; i < 10; i++ {
		duration := float64(i) * 0.1 // 0.0, 0.1, 0.2, ... 0.9 seconds
		histogram.Record(ctx, duration, metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String("GET"),
		))
		fmt.Printf("  Recorded: %.1fs\n", duration)
		time.Sleep(100 * time.Millisecond)
	}
	
	fmt.Println("⏳ Waiting 10 seconds for export...")
	time.Sleep(10 * time.Second)
	
	fmt.Println("🔍 Check metrics at: http://localhost:8889/metrics")
	fmt.Println("🔍 Look for: simple_test_duration_seconds")
	
	// Shutdown
	if err := mp.Shutdown(context.Background()); err != nil {
		log.Printf("Error shutting down: %v", err)
	}
}