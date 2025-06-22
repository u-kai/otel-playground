package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ExemplarResponse struct {
	Status string `json:"status"`
	Data   []struct {
		SeriesLabels map[string]string `json:"seriesLabels"`
		Exemplars    []struct {
			Labels map[string]string `json:"labels"`
			Value  string            `json:"value"`
			Timestamp float64        `json:"timestamp"`
		} `json:"exemplars"`
	} `json:"data"`
}

func main() {
	fmt.Println("🧪 Testing Complete Exemplar Flow")
	fmt.Println("=" + string(make([]rune, 50)))
	
	// Generate some traffic
	fmt.Println("1️⃣ Generating traffic with trace context...")
	for i := 1; i <= 3; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:8080/users?id=%d", i))
		if err != nil {
			fmt.Printf("   ❌ Request %d failed: %v\n", i, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("   ✅ Request %d: %d\n", i, resp.StatusCode)
		time.Sleep(500 * time.Millisecond)
	}
	
	// Wait for metrics to be collected
	fmt.Println("\n2️⃣ Waiting for metrics collection...")
	time.Sleep(8 * time.Second)
	
	// Check if exemplars exist in Prometheus
	fmt.Println("3️⃣ Checking Prometheus exemplars...")
	resp, err := http.Get("http://localhost:9090/api/v1/query_exemplars?query=microservices_user_service_requests_total")
	if err != nil {
		fmt.Printf("   ❌ Failed to query exemplars: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("   ❌ Failed to read response: %v\n", err)
		return
	}
	
	var exemplarResp ExemplarResponse
	if err := json.Unmarshal(body, &exemplarResp); err != nil {
		fmt.Printf("   ❌ Failed to parse response: %v\n", err)
		return
	}
	
	if exemplarResp.Status != "success" {
		fmt.Printf("   ❌ API returned error status: %s\n", exemplarResp.Status)
		return
	}
	
	if len(exemplarResp.Data) == 0 {
		fmt.Println("   ❌ No exemplar data found")
		return
	}
	
	fmt.Printf("   ✅ Found %d series with exemplars\n", len(exemplarResp.Data))
	
	for i, series := range exemplarResp.Data {
		fmt.Printf("\n   📊 Series %d:\n", i+1)
		fmt.Printf("      Route: %s\n", series.SeriesLabels["http_route"])
		fmt.Printf("      Method: %s\n", series.SeriesLabels["http_request_method"])
		fmt.Printf("      Exemplars: %d\n", len(series.Exemplars))
		
		for j, exemplar := range series.Exemplars {
			fmt.Printf("      🔗 Exemplar %d:\n", j+1)
			fmt.Printf("         Trace ID: %s\n", exemplar.Labels["trace_id"])
			fmt.Printf("         Span ID: %s\n", exemplar.Labels["span_id"])
			fmt.Printf("         Value: %s\n", exemplar.Value)
			fmt.Printf("         Jaeger Link: http://localhost:16686/trace/%s\n", 
				exemplar.Labels["trace_id"])
		}
	}
	
	fmt.Println("\n4️⃣ Testing complete! 🎉")
	fmt.Println("\n🔗 To see exemplars in action:")
	fmt.Println("   📊 Open Grafana: http://localhost:3000 (admin/admin)")
	fmt.Println("   📈 Open 'OpenTelemetry Exemplars Demo' dashboard")
	fmt.Println("   🔍 Look for exemplar dots on the graphs")
	fmt.Println("   🎯 Click exemplar dots to jump to Jaeger traces")
}