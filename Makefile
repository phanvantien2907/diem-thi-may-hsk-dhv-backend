.PHONY: run build test loadtest-setup loadtest-run dev down

# Sinh file Swagger Docs
swag:
	go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/api/main.go

# Chạy API Server (tự động cập nhật docs)
run: swag
	go run cmd/api/main.go

# Chế độ Dev (nếu bạn có dùng air để live-reload)
dev:
	air

# Build ra file thực thi
build:
	go build -o bin/api cmd/api/main.go

# Chạy test toàn bộ dự án
test:
	go test -v ./...

# Chạy setup dữ liệu k6 (Tạo 100k users)
loadtest-setup:
	cd scripts && go run k6_setup.go

# Bắn test k6
loadtest-run:
	cd scripts && k6 run k6_test.js

# Khởi động Database bằng Docker
db-up:
	docker-compose up -d

# Tắt Database
db-down:
	docker-compose down
