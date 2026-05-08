#!/bin/bash
set -e

# ============================================
# 秒杀系统一键部署脚本
# 使用: ./deploy.sh
# ============================================

echo "=========================================="
echo "  秒杀系统部署脚本"
echo "=========================================="

# 配置
APP_NAME="seckill"
WEB_DIR="/app/seckill-web"
APP_DIR="/app/seckill"
JWT_SECRET=${JWT_SECRET:-"your-secret-key-change-in-production"}

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# 检查 Docker
if ! command -v docker &> /dev/null; then
    error "Docker 未安装，请先安装 Docker"
fi

if ! command -v docker-compose &> /dev/null; then
    error "docker-compose 未安装，请先安装"
fi

# 创建目录
info "创建应用目录..."
sudo mkdir -p $WEB_DIR
sudo mkdir -p $APP_DIR/logs

# 复制前端文件
info "部署前端页面..."
if [ -d "$(pwd)/web" ]; then
    sudo cp -r "$(pwd)/web/"* "$WEB_DIR/"
    info "前端文件已复制到 $WEB_DIR"
else
    warn "未找到 web 目录，跳过前端部署"
fi

# 生成 docker-compose.yml
info "生成 docker-compose.yml..."
cat > $APP_DIR/docker-compose.yml << 'EOF'
version: '3.8'

services:
  # MySQL 数据库
  mysql:
    image: mysql:8.0
    container_name: seckill-mysql
    restart: unless-stopped
    ports:
      - "3306:3306"
    environment:
      - MYSQL_ROOT_PASSWORD=seckill123
      - MYSQL_DATABASE=seckill
    volumes:
      - mysql-data:/var/lib/mysql
      - ./scripts/init.sql:/docker-entrypoint-initdb.d/init.sql
    networks:
      - seckill-net
    command: --default-authentication-plugin=mysql_native_password

  # Redis 缓存
  redis:
    image: redis:7-alpine
    container_name: seckill-redis
    restart: unless-stopped
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    networks:
      - seckill-net

  # 秒杀应用
  seckill-app:
    build:
      context: .
      dockerfile: deploy/Dockerfile
    container_name: seckill-app
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - SECKILL_ENV=prod
      - MYSQL_HOST=mysql
      - MYSQL_USER=root
      - MYSQL_PASSWORD=seckill123
      - MYSQL_DATABASE=seckill
      - REDIS_HOST=redis
      - REDIS_PASSWORD=
      - JWT_SECRET=${JWT_SECRET}
    depends_on:
      - mysql
      - redis
    volumes:
      - ./logs:/app/logs
    networks:
      - seckill-net

  # Nginx 反向代理
  nginx:
    image: nginx:alpine
    container_name: seckill-nginx
    restart: unless-stopped
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
      - ./seckill-web:/app/seckill-web:ro
    depends_on:
      - seckill-app
    networks:
      - seckill-net

volumes:
  mysql-data:
  redis-data:

networks:
  seckill-net:
    driver: bridge
EOF

info "docker-compose.yml 已生成"

# 复制必要文件
info "复制配置文件..."
cp -r $(pwd)/scripts $APP_DIR/
cp $(pwd)/deploy/nginx.conf $APP_DIR/
cp $(pwd)/go.mod $APP_DIR/ 2>/dev/null || true
cp $(pwd)/go.sum $APP_DIR/ 2>/dev/null || true

# 启动服务
info "启动服务（MySQL + Redis + App + Nginx）..."
cd $APP_DIR
docker-compose up -d --build

# 等待 MySQL 启动
info "等待 MySQL 初始化..."
sleep 15

# 初始化数据库（如果需要）
info "检查数据库状态..."
for i in {1..30}; do
    if docker exec seckill-mysql mysql -uroot -pseckill123 -e "SELECT 1" &>/dev/null; then
        info "MySQL 已就绪"
        break
    fi
    if [ $i -eq 30 ]; then
        error "MySQL 启动超时"
    fi
    sleep 2
done

# 预热 Redis 库存
info "预热商品数据..."
docker exec seckill-app ./seckill -init-stock 1 10000 || true

# 显示状态
echo ""
echo "=========================================="
echo -e "${GREEN}  部署完成！${NC}"
echo "=========================================="
echo ""
echo "服务状态:"
docker-compose ps
echo ""
echo "访问地址:"
echo "  前端页面: http://你的服务器IP/"
echo "  API 接口: http://你的服务器IP/api/v1/"
echo "  健康检查: http://你的服务器IP/health"
echo ""
echo "测试账号:"
echo "  手机号: 13800000000"
echo "  密码:   admin123"
echo ""
echo "管理命令:"
echo "  查看日志: docker-compose logs -f seckill-app"
echo "  重启服务: docker-compose restart"
echo "  停止服务: docker-compose down"
echo ""
echo "=========================================="