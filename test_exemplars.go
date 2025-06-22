package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TestExemplarsImplementation tests Views and Exemplars functionality
func main() {
	fmt.Println("🔬 Testing OpenTelemetry Views and Exemplars Implementation")
	fmt.Println(strings.Repeat("=", 60))
	
	// Wait for services to start
	fmt.Println("⏳ Waiting for services to be ready...")
	time.Sleep(5 * time.Second)
	
	// Test metrics endpoints
	testMetricsEndpoints()
	
	// Generate test traffic with different latencies
	generateTestTraffic()
	
	// Wait for metrics to be collected
	fmt.Println("⏳ Waiting for metrics collection...")
	time.Sleep(10 * time.Second)
	
	// Check for histogram metrics
	checkHistogramMetrics()
	
	// Check for exemplars
	checkExemplars()
	
	fmt.Println("\n✅ Test completed!")
}

func testMetricsEndpoints() {
	fmt.Println("\n📊 Testing Metrics Endpoints:")
	
	endpoints := []struct {
		name string
		url  string
	}{
		{"User Service Health", "http://localhost:8080/health"},
		{"OTEL Collector Metrics", "http://localhost:8889/metrics"},
		{"Prometheus API", "http://localhost:9090/api/v1/label/__name__/values"},
	}
	
	for _, endpoint := range endpoints {
		resp, err := http.Get(endpoint.url)
		if err != nil {
			fmt.Printf("  ❌ %s: %v\n", endpoint.name, err)
			continue
		}
		defer resp.Body.Close()
		
		if resp.StatusCode == 200 {
			fmt.Printf("  ✅ %s: OK\n", endpoint.name)
		} else {
			fmt.Printf("  ❌ %s: HTTP %d\n", endpoint.name, resp.StatusCode)
		}
	}
}

func generateTestTraffic() {
	fmt.Println("\n🚦 Generating Test Traffic for Views & Exemplars:")
	
	scenarios := []struct {
		name   string
		userID int
		desc   string
	}{
		{"Normal Request", 1, "Should be fast (<50ms)"},
		{"Medium Latency", 100, "Should have 200ms delay"},
		{"High Latency (Exemplar)", 999, "Should have 2s delay + exemplar"},
		{"Error Request", 9999, "Should return 404"},
	}
	
	for _, scenario := range scenarios {
		fmt.Printf("  %s (ID=%d): %s\n", scenario.name, scenario.userID, scenario.desc)
		
		start := time.Now()
		resp, err := http.Get(fmt.Sprintf("http://localhost:8080/users?id=%d", scenario.userID))
		duration := time.Since(start)
		
		if err != nil {
			fmt.Printf("    ❌ Error: %v\n", err)
			continue
		}
		defer resp.Body.Close()
		
		fmt.Printf("    ⏱️  Duration: %.3fs, Status: %d\n", duration.Seconds(), resp.StatusCode)
	}
}

func checkHistogramMetrics() {
	fmt.Println("\n📈 Checking Histogram Metrics:")
	
	resp, err := http.Get("http://localhost:8889/metrics")
	if err != nil {
		fmt.Printf("  ❌ Failed to get metrics: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("  ❌ Failed to read metrics: %v\n", err)
		return
	}
	
	content := string(body)
	
	// Check for our custom histogram
	if strings.Contains(content, "user_service_response_time_custom") {
		fmt.Println("  ✅ Custom histogram metric found!")
		
		// Extract histogram buckets
		bucketRegex := regexp.MustCompile(`user_service_response_time_custom_bucket\{.*le="([^"]+)".*\}\s+(\d+)`)
		matches := bucketRegex.FindAllStringSubmatch(content, -1)
		
		if len(matches) > 0 {
			fmt.Println("    📊 Histogram buckets:")
			for _, match := range matches {
				if len(match) >= 3 {
					fmt.Printf("      le=%s: %s samples\n", match[1], match[2])
				}
			}
		}
	} else {
		fmt.Println("  ❌ Custom histogram metric NOT found")
		
		// List what metrics we do have
		lines := strings.Split(content, "\n")
		fmt.Println("    Available metrics:")
		for _, line := range lines {
			if strings.HasPrefix(line, "microservices_user_service") && !strings.HasPrefix(line, "#") {
				fmt.Printf("      %s\n", strings.Split(line, " ")[0])
			}
		}
	}
}

func checkExemplars() {
	fmt.Println("\n🔗 Checking for Exemplars:")
	
	resp, err := http.Get("http://localhost:8889/metrics")
	if err != nil {
		fmt.Printf("  ❌ Failed to get metrics: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("  ❌ Failed to read metrics: %v\n", err)
		return
	}
	
	content := string(body)
	
	// Look for exemplar patterns (trace_id, span_id)
	exemplarPatterns := []string{
		"trace_id",
		"span_id", 
		"# {",  // OpenMetrics exemplar format
	}
	
	foundExemplars := false
	for _, pattern := range exemplarPatterns {
		if strings.Contains(content, pattern) {
			foundExemplars = true
			fmt.Printf("  ✅ Found exemplar indicator: %s\n", pattern)
		}
	}
	
	if !foundExemplars {
		fmt.Println("  ❌ No exemplars found in metrics output")
		fmt.Println("    This might indicate:")
		fmt.Println("    - Exemplars not properly configured")
		fmt.Println("    - Not enough trace context during metric recording")
		fmt.Println("    - OTLP exporter doesn't support exemplars yet")
	} else {
		fmt.Println("  🎯 Exemplars appear to be working!")
	}
}