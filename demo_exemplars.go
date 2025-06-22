package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	fmt.Println("🎬 OpenTelemetry Exemplars Demo")
	fmt.Println("=" + string(make([]rune, 50)))
	
	fmt.Println("\n🚀 Step 1: Generating Diverse Traffic Patterns")
	fmt.Println("   This creates different scenarios to showcase exemplars...")
	
	// Scenario 1: Normal requests
	fmt.Print("   📊 Normal requests: ")
	for i := 1; i <= 3; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:8080/users?id=%d", i))
		if err == nil {
			resp.Body.Close()
			fmt.Print("✅ ")
		} else {
			fmt.Print("❌ ")
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Println()
	
	// Scenario 2: Slow requests (for interesting exemplars)
	fmt.Print("   🐌 Slow requests (user 999): ")
	resp, err := http.Get("http://localhost:8080/users?id=999")
	if err == nil {
		resp.Body.Close()
		fmt.Print("✅ (2s delay)")
	} else {
		fmt.Print("❌")
	}
	fmt.Println()
	
	// Scenario 3: Error requests
	fmt.Print("   ❌ Error requests: ")
	for i := 9990; i <= 9992; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:8080/users?id=%d", i))
		if err == nil {
			resp.Body.Close()
			fmt.Print("🔴 ")
		} else {
			fmt.Print("❌ ")
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println()
	
	fmt.Println("\n⏳ Step 2: Waiting for Metrics Collection & Export...")
	time.Sleep(10 * time.Second)
	
	fmt.Println("\n🔍 Step 3: Checking Exemplar Storage in Prometheus")
	
	// Check exemplars API
	resp, err = http.Get("http://localhost:9090/api/v1/query_exemplars?query=microservices_user_service_requests_total")
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
	
	var exemplarData struct {
		Status string `json:"status"`
		Data   []struct {
			Exemplars []struct {
				Labels map[string]string `json:"labels"`
				Value  string            `json:"value"`
			} `json:"exemplars"`
		} `json:"data"`
	}
	
	if err := json.Unmarshal(body, &exemplarData); err != nil {
		fmt.Printf("   ❌ Failed to parse exemplars: %v\n", err)
		return
	}
	
	if len(exemplarData.Data) == 0 || len(exemplarData.Data[0].Exemplars) == 0 {
		fmt.Println("   ❌ No exemplars found")
		return
	}
	
	exemplarCount := len(exemplarData.Data[0].Exemplars)
	fmt.Printf("   ✅ Found %d exemplars stored in Prometheus!\n", exemplarCount)
	
	// Show a few exemplar trace IDs
	for i, exemplar := range exemplarData.Data[0].Exemplars {
		if i >= 3 {
			break // Show only first 3
		}
		fmt.Printf("   🔗 Exemplar %d: Trace %s\n", i+1, exemplar.Labels["trace_id"])
	}
	
	fmt.Println("\n🎯 Step 4: Live Demo Instructions")
	fmt.Println("   Follow these steps to see exemplars in action:")
	fmt.Println()
	
	fmt.Println("   A) 📊 Grafana Dashboard (Recommended)")
	fmt.Printf("      → Open: http://localhost:3000\n")
	fmt.Printf("      → Login: admin / admin\n")
	fmt.Printf("      → Navigate to 'OpenTelemetry Exemplars Demo' dashboard\n")
	fmt.Printf("      → Look for DOTS on the time series graphs\n")
	fmt.Printf("      → Click any dot → Jumps to Jaeger trace!\n")
	fmt.Println()
	
	fmt.Println("   B) 📈 Prometheus UI")
	fmt.Printf("      → Open: http://localhost:9090\n")
	fmt.Printf("      → Query: microservices_user_service_requests_total\n")
	fmt.Printf("      → Switch to Graph view\n")
	fmt.Printf("      → Look for exemplar markers\n")
	fmt.Println()
	
	fmt.Println("   C) 🔍 Jaeger Traces")
	fmt.Printf("      → Open: http://localhost:16686\n")
	fmt.Printf("      → Search for service: user-service\n")
	fmt.Printf("      → See traces created by the demo\n")
	fmt.Println()
	
	fmt.Println("🎉 Demo Complete!")
	fmt.Println("   The exemplars are linking metrics → traces successfully!")
	fmt.Printf("   Try clicking exemplar dots in Grafana to experience the magic! ✨\n")
}