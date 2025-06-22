package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	fmt.Println("🔍 Exemplar Verification Tool")
	fmt.Println("=" + string(make([]rune, 40)))
	
	// Step 1: Generate fresh traffic
	fmt.Println("\n1️⃣ Generating fresh exemplar traffic...")
	testUserID := int(time.Now().Unix() % 1000)
	
	resp, err := http.Get(fmt.Sprintf("http://localhost:8080/users?id=%d", testUserID))
	if err != nil {
		fmt.Printf("❌ Failed to generate traffic: %v\n", err)
		return
	}
	resp.Body.Close()
	fmt.Printf("✅ Generated request for user %d\n", testUserID)
	
	// Step 2: Wait for collection
	fmt.Println("\n2️⃣ Waiting for metrics collection...")
	time.Sleep(8 * time.Second)
	
	// Step 3: Get exemplars
	fmt.Println("3️⃣ Checking exemplar storage...")
	resp, err = http.Get("http://localhost:9090/api/v1/query_exemplars?query=microservices_user_service_requests_total")
	if err != nil {
		fmt.Printf("❌ Failed to query exemplars: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Failed to read response: %v\n", err)
		return
	}
	
	var exemplarData struct {
		Status string `json:"status"`
		Data   []struct {
			Exemplars []struct {
				Labels map[string]string `json:"labels"`
				Value  string            `json:"value"`
				Timestamp float64        `json:"timestamp"`
			} `json:"exemplars"`
		} `json:"data"`
	}
	
	if err := json.Unmarshal(body, &exemplarData); err != nil {
		fmt.Printf("❌ Failed to parse exemplars: %v\n", err)
		return
	}
	
	if len(exemplarData.Data) == 0 || len(exemplarData.Data[0].Exemplars) == 0 {
		fmt.Println("❌ No exemplars found")
		return
	}
	
	// Get latest exemplar
	latest := exemplarData.Data[0].Exemplars[len(exemplarData.Data[0].Exemplars)-1]
	traceID := latest.Labels["trace_id"]
	spanID := latest.Labels["span_id"]
	
	fmt.Printf("✅ Latest exemplar found!\n")
	fmt.Printf("   Trace ID: %s\n", traceID)
	fmt.Printf("   Span ID: %s\n", spanID)
	
	// Step 4: Test Jaeger link
	fmt.Println("\n4️⃣ Testing Jaeger link...")
	jaegerURL := fmt.Sprintf("http://localhost:16686/api/traces/%s", traceID)
	resp, err = http.Get(jaegerURL)
	if err != nil {
		fmt.Printf("❌ Jaeger link failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 200 {
		fmt.Printf("✅ Jaeger trace accessible!\n")
	} else {
		fmt.Printf("❌ Jaeger returned status: %d\n", resp.StatusCode)
		return
	}
	
	// Step 5: Instructions
	fmt.Println("\n🎯 Manual Testing Instructions:")
	fmt.Printf("   📊 Grafana Dashboard: http://localhost:3000\n")
	fmt.Printf("   🔗 Direct Jaeger Link: http://localhost:16686/trace/%s\n", traceID)
	fmt.Printf("   📈 Prometheus Query: http://localhost:9090/graph?g0.expr=microservices_user_service_requests_total&g0.tab=0\n")
	
	fmt.Println("\n✅ Exemplar system is working correctly!")
	fmt.Println("   Try the links above to see exemplars in action! 🚀")
}