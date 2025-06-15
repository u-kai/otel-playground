.PHONY: help up down restart run logs clean

# デフォルトターゲット
help:
	@echo "Available commands:"
	@echo "  make up      - Start Jaeger and PostgreSQL services"
	@echo "  make down    - Stop all services"
	@echo "  make restart - Restart all services"
	@echo "  make run     - Run the Go application with tracing"
	@echo "  make logs    - Show container logs"
	@echo "  make clean   - Stop services and remove volumes"
	@echo "  make jaeger  - Open Jaeger UI in browser"

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

# アプリケーション実行（環境変数付き）
run:
	@echo "🚀 Running application with OpenTelemetry tracing..."
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

# ヘルスチェック
status:
	@echo "=== Service Status ==="
	@docker-compose ps
	@echo ""
	@echo "=== Port Check ==="
	@echo "PostgreSQL (5432):"
	@lsof -i :5432 || echo "  Not listening"
	@echo "Jaeger UI (16686):"
	@lsof -i :16686 || echo "  Not listening" 
	@echo "OTLP Receiver (4318):"
	@lsof -i :4318 || echo "  Not listening"