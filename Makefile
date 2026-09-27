.PHONY: run build test loadtest-setup loadtest-run dev down

# Sinh file Swagger Docs
sg:
	go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/api/main.go

# Chạy API Server (tự động cập nhật docs)
r: sg
	go run cmd/api/main.go

# Chế độ Dev (nếu bạn có dùng air để live-reload)
d:
	air

# Build ra file thực thi
b:
	go build -o bin/api cmd/api/main.go

# Chạy test toàn bộ dự án
t:
	go test -v ./...

# Chạy setup dữ liệu k6 (Tạo 100k users)
lts:
	cd scripts && go run k6_setup.go

# Bắn test k6
lt:
	cd scripts && k6 run k6_test.js

# Chạy setup dữ liệu k6 cho Payment Webhook
ltps:
	cd scripts && go run k6_payment_setup.go

# Bắn test k6 cho Payment Webhook
ltp:
	cd scripts && k6 run k6_payment_test.js

# Chạy setup dữ liệu Flash Sale (5 ghế, 2000 users)
ltfs:
	cd scripts && go run k6_flashsale_setup.go

# Bắn test k6 Flash Sale + Thanh toán
ltf:
	cd scripts && k6 run k6_flashsale_test.js

# Bắn test k6 Spam Click (Spam tạo link thanh toán)
ltsp:
	cd scripts && k6 run k6_spamclick_test.js

# Bắn test k6 Nâng cao (Bảo mật & Cronjob Clash)
ltadv:
	cd scripts && k6 run k6_advanced_test.js

# Khởi động Database bằng Docker
dbu:
	docker-compose up -d

# Tắt Database (KHÔNG xóa dữ liệu)
dbd:
	docker-compose down

# Khởi động lại Database (KHÔNG xóa dữ liệu)
dbr:
	docker-compose restart

# Xóa hoàn toàn Database và khởi tạo lại từ đầu (Xóa Volume)
db-rs:
	docker-compose down -v
	docker-compose up -d

# Tắt và Khởi động lại Database (KHÔNG xóa Volume)
db-reup:
	docker-compose down
	docker-compose up -d

# Đóng gói lại (build) code mới và chạy lại Docker
db-build:
	docker-compose up --build -d


# ── Migrations ─────────────────────────────────────────────────────────────
MIGRATE_DSN="postgres://postgres:postgres@localhost:5432/hsk_vinh?sslmode=disable"

mig-up:
	migrate -path migrations -database $(MIGRATE_DSN) up

mig-down:
	migrate -path migrations -database $(MIGRATE_DSN) down -all

mig-create:
	@read -p "Enter migration name: " name; \
	migrate create -ext sql -dir migrations -seq $$name
