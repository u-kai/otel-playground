.PHONY: help up down restart run logs clean services demo stop-services

# デフォルトターゲット
help:
	@echo "Available commands:"
	@echo "🚀 Quick Start:"
	@echo "  make demo             - Full demo: start all services + run orchestrator"
	@echo "  make services         - Start all microservices (user + post)"
	@echo ""
	@echo "🔧 Individual Commands:"
	@echo "  make up               - Start Jaeger and PostgreSQL services"
	@echo "  make down             - Stop all services"
	@echo "  make restart          - Restart all services"
	@echo "  make run              - Run integrated demo application"
	@echo "  make run-orchestrator - Run microservice orchestrator"
	@echo "  make user-service     - Start user service API (port 8080)"
	@echo "  make post-service     - Start post service API (port 8081)"
	@echo "  make logs             - Show container logs"
	@echo "  make clean            - Stop services and remove volumes"
	@echo "  make jaeger           - Open Jaeger UI in browser"

# サービス起動
up:
	docker-compose up -d
	@echo "✅ Services started! Waiting for PostgreSQL to be ready..."
	@sleep 10
	@echo "✅ Ready! Run 'make run' to execute the application"

# サービス停止
down:
	docker-compose down

# サービス再起動
restart: down up

# アプリケーション実行（環境変数付き） - 統合デモ
run:
	@echo "🚀 Running integrated demo application..."
	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run main.go
	@echo ""
	@echo "📊 View traces at: http://localhost:16686"

# ログ確認
logs:
	docker-compose logs -f

# PostgreSQLログのみ
logs-db:
	docker-compose logs -f postgres

# Jaegerログのみ
logs-jaeger:
	docker-compose logs -f jaeger

# 完全クリーンアップ
clean:
	docker-compose down -v
	@echo "✅ All services stopped and volumes removed"

# Jaeger UI をブラウザで開く
jaeger:
	@echo "Opening Jaeger UI..."
	@open http://localhost:16686 || echo "Please open http://localhost:16686 manually"

# 開発用: サービス起動→アプリ実行
dev: up run

# ユーザーサービス起動
user-service:
	@echo "🚀 Starting user service..."
	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run cmd/user/main.go

# 投稿サービス起動
post-service:
	@echo "🚀 Starting post service..."
	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run cmd/post/main.go

# マイクロサービスオーケストレーター（要：user-service, post-service起動）
run-orchestrator:
	@echo "🚀 Running microservice orchestrator..."
	@echo "⚠️  Make sure user-service and post-service are running first!"
	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run main.go
	@echo ""
	@echo "📊 View end-to-end traces at: http://localhost:16686"

# 全マイクロサービスを並行起動（バックグラウンド）
services: up
	@echo "🚀 Starting all microservices..."
	@echo "📊 Starting user-service on port 8080..."
	@(OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run cmd/user/main.go) & \
	echo "📊 Starting post-service on port 8081..." && \
	(OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run cmd/post/main.go) & \
	echo "✅ All services started in background!" && \
	echo "🔍 Check status: make status" && \
	echo "🛑 Stop all: make stop-services"

# バックグラウンドサービスを停止
stop-services:
	@echo "🛑 Stopping all background services..."
	@pkill -f "go run cmd/user/main.go" || true
	@pkill -f "go run cmd/post/main.go" || true
	@echo "✅ All services stopped!"

# フルデモ：インフラ起動 → サービス起動 → オーケストレーター実行
demo: up
	@echo "🎬 Starting full microservices demo..."
	@echo "📊 Step 1: Starting microservices..."
	@(OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run cmd/user/main.go) & \
	USER_PID=$$!; \
	(OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run cmd/post/main.go) & \
	POST_PID=$$!; \
	echo "⏳ Waiting for services to start..." && \
	sleep 5 && \
	echo "📊 Step 2: Running orchestrator..." && \
	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run main.go; \
	echo "🛑 Stopping services..." && \
	kill $$USER_PID $$POST_PID 2>/dev/null || true
	@echo "🎉 Demo completed! Check traces at http://localhost:16686"

# ヘルスチェック
status:
	@echo "=== Docker Services ==="
	@docker-compose ps
	@echo ""
	@echo "=== Port Status ==="
	@echo "PostgreSQL (5432):"
	@lsof -i :5432 || echo "  ❌ Not listening"
	@echo "Jaeger UI (16686):"
	@lsof -i :16686 || echo "  ❌ Not listening" 
	@echo "OTLP Receiver (4318):"
	@lsof -i :4318 || echo "  ❌ Not listening"
	@echo "User Service (8080):"
	@lsof -i :8080 || echo "  ❌ Not listening"
	@echo "Post Service (8081):"
	@lsof -i :8081 || echo "  ❌ Not listening"
	@echo ""
	@echo "=== Microservice Health Check ==="
	@curl -s http://localhost:8080/health 2>/dev/null | jq -r '.status // "❌ user-service not responding"' || echo "❌ user-service not responding"
	@curl -s http://localhost:8081/health 2>/dev/null | jq -r '.status // "❌ post-service not responding"' || echo "❌ post-service not responding"