# Makefile for testing

.PHONY: test unit-test integration-test e2e-test benchmark pprof clean

# 单元测试
unit-test:
	go test ./... -v -cover

# 单元测试（带覆盖率）
unit-test-cover:
	go test ./... -v -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# 集成测试（需要先启动 docker-compose）
integration-test:
	docker-compose -f docker-compose.test.yml up -d
	sleep 10
	go test ./integration/... -v
	docker-compose -f docker-compose.test.yml down

# E2E 测试
e2e-test:
	./scripts/e2e-test.sh

# 压测（WRK）
benchmark-wrk:
	wrk -t4 -c100 -d30s http://localhost:8080/api/v1/seckill/1

# 压测（Vegeta）
benchmark-vegeta:
	./scripts/vegeta-test.sh

# pprof CPU 分析
pprof-cpu:
	./scripts/pprof-profile.sh cpu 30

# pprof 内存分析
pprof-mem:
	./scripts/pprof-profile.sh mem

# pprof Goroutine 分析
pprof-goroutine:
	./scripts/pprof-profile.sh goroutine

# 启动测试环境
test-up:
	docker-compose -f docker-compose.test.yml up -d

# 停止测试环境
test-down:
	docker-compose -f docker-compose.test.yml down

# 清理
clean:
	rm -rf coverage.html coverage.out
	rm -rf pprof-results/
	rm -rf results/
	docker-compose -f docker-compose.test.yml down -v

# 运行所有测试
test: unit-test
