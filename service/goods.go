package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"seckill/model"
	"seckill/repository"

	"gorm.io/gorm"
)

// 缓存配置
const (
	goodsListCacheKey = "goods:list:page:%d:size:%d"
	goodsTotalCacheKey = "goods:total"
	goodsListCacheTTL  = 30 * time.Second
	goodsTotalCacheTTL = 30 * time.Second
)

type GoodsService struct {
	db *gorm.DB
}

func NewGoodsService(db *gorm.DB) *GoodsService {
	return &GoodsService{
		db: db,
	}
}

func (s *GoodsService) CreateGoods(goods *model.SeckillGoods) error {
	return s.db.Create(goods).Error
}

func (s *GoodsService) GetGoodsByID(id uint) (*model.SeckillGoods, error) {
	var goods model.SeckillGoods
	err := s.db.First(&goods, id).Error
	if err != nil {
		return nil, err
	}
	return &goods, nil
}

// GetGoodsList 获取商品列表（带缓存和实时库存，Redis故障时降级到MySQL）
func (s *GoodsService) GetGoodsList(ctx context.Context, page, pageSize int) ([]model.SeckillGoods, int64, error) {
	offset := (page - 1) * pageSize
	cacheKey := fmt.Sprintf(goodsListCacheKey, page, pageSize)

	// 1. 尝试从缓存获取列表
	cached, err := s.getListFromCache(ctx, cacheKey)
	if err == nil && cached != nil {
		// 缓存命中，获取实时库存
		for i := range cached {
			stock, err := s.GetTotalStockFromBuckets(cached[i].ID)
			if err != nil || (stock == 0 && cached[i].Stock > 0) {
				stock = cached[i].Stock
			}
			cached[i].Stock = stock
		}
		// 异步更新 total
		go func() {
			total, _ := s.getTotalFromCache(context.Background())
			_ = total
		}()
		return cached, 0, nil
	}

	// 2. 缓存未命中或Redis故障，查询数据库
	var goodsList []model.SeckillGoods

	// 查询进行中的秒杀活动（状态=1 且 时间有效）
	if err := s.db.WithContext(ctx).
		Where("status = 1 AND start_time <= ? AND end_time > ?", time.Now(), time.Now()).
		Order("start_time ASC").
		Offset(offset).Limit(pageSize).
		Find(&goodsList).Error; err != nil {
		return nil, 0, err
	}

	// 3. 获取实时库存（优先Redis，降级到MySQL）
	for i := range goodsList {
		stock, err := s.GetTotalStockFromBuckets(goodsList[i].ID)
		// Redis故障 或 桶不存在（返回0但MySQL有库存）时使用MySQL库存
		if err != nil || (stock == 0 && goodsList[i].Stock > 0) {
			stock = goodsList[i].Stock
		}
		goodsList[i].Stock = stock
	}

	// 4. 尝试写入列表缓存（不阻塞，失败忽略）
	s.setListToCache(ctx, cacheKey, goodsList)

	// 5. 获取总数（优先缓存，降级到MySQL）
	total, err := s.getTotalFromCache(ctx)
	if err != nil {
		// Redis故障，直接查询MySQL
		s.db.WithContext(ctx).
			Model(&model.SeckillGoods{}).
			Where("status = 1 AND start_time <= ? AND end_time > ?", time.Now(), time.Now()).
			Count(&total)
	}

	return goodsList, total, nil
}

// GetGoodsListSimple 获取商品列表（不带缓存，用于管理后台）
func (s *GoodsService) GetGoodsListSimple(page, pageSize int) ([]model.SeckillGoods, int64, error) {
	var goodsList []model.SeckillGoods
	var total int64

	offset := (page - 1) * pageSize

	if err := s.db.Model(&model.SeckillGoods{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := s.db.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&goodsList).Error; err != nil {
		return nil, 0, err
	}

	return goodsList, total, nil
}

func (s *GoodsService) UpdateStock(id uint, stock int) error {
	return s.db.Model(&model.SeckillGoods{}).Where("id = ?", id).Update("stock", stock).Error
}

// GetStock 获取实时库存（优先 Redis）
func (s *GoodsService) GetStock(ctx context.Context, skuID uint) (int, error) {
	stockKey := repository.StockKey(strconv.Itoa(int(skuID)))

	// 先查 Redis
	stock, err := repository.Redis.Get(ctx, stockKey).Int()
	if err == nil {
		return stock, nil
	}

	// Redis 没有，查 MySQL
	goods, err := s.GetGoodsByID(skuID)
	if err != nil {
		return 0, err
	}

	// 回填 Redis（使用短超时避免阻塞）
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	repository.Redis.SetEx(timeoutCtx, stockKey, goods.Stock, 1*time.Hour)

	return goods.Stock, nil
}

// PreHeatStock 预热库存到 Redis（单桶模式）
func (s *GoodsService) PreHeatStock(skuID uint) error {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	goods, err := s.GetGoodsByID(skuID)
	if err != nil {
		return err
	}

	// 预热库存
	stockKey := repository.StockKey(skuStr)
	if err := repository.Redis.SetEx(ctx, stockKey, goods.Stock, 24*time.Hour).Err(); err != nil {
		return err
	}

	// 预热商品信息缓存（用于秒杀时快速获取商品状态）
	goodsJSON, err := json.Marshal(goods)
	if err == nil {
		repository.SetGoods(ctx, skuStr, string(goodsJSON), 24*time.Hour)
	}

	return nil
}

// PreHeatStockWithBuckets 预热库存到 Redis（分桶模式）
// 将库存平均分配到多个桶，减少单 key 热点竞争
// 库存分桶后，每个桶的库存 = 总库存 / 桶数
func (s *GoodsService) PreHeatStockWithBuckets(skuID uint) error {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))

	goods, err := s.GetGoodsByID(skuID)
	if err != nil {
		return err
	}

	// 分桶预热库存（使用 Pipeline 批量操作，减少 RTT）
	_, err = repository.PreHeatStockToBuckets(ctx, skuStr, goods.Stock, StockBucketCount, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("预热分桶库存失败: %w", err)
	}

	// 预热商品信息缓存（用于秒杀时快速获取商品状态）
	goodsJSON, err := json.Marshal(goods)
	if err == nil {
		repository.SetGoods(ctx, skuStr, string(goodsJSON), 24*time.Hour)
	}

	return nil
}

// GetTotalStockFromBuckets 获取所有桶的总库存（用于管理后台展示）
func (s *GoodsService) GetTotalStockFromBuckets(skuID uint) (int, error) {
	ctx := context.Background()
	skuStr := strconv.Itoa(int(skuID))
	return repository.GetTotalStockFromBuckets(ctx, skuStr, StockBucketCount)
}

// InvalidateGoodsListCache 作废商品列表缓存
func (s *GoodsService) InvalidateGoodsListCache() error {
	ctx := context.Background()

	// 删除总数缓存
	repository.Delete(ctx, goodsTotalCacheKey)

	// 列表缓存使用通配符删除（Redis SCAN 或手动删除常见分页）
	// 这里简化处理，缓存 30 秒后自动过期
	return nil
}

// getListFromCache 从缓存获取列表
func (s *GoodsService) getListFromCache(ctx context.Context, key string) ([]model.SeckillGoods, error) {
	data, err := repository.Redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var list []model.SeckillGoods
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// setListToCache 设置列表缓存
func (s *GoodsService) setListToCache(ctx context.Context, key string, list []model.SeckillGoods) {
	data, err := json.Marshal(list)
	if err != nil {
		return
	}
	repository.SetWithExpire(ctx, key, data, goodsListCacheTTL)
}

// getTotalFromCache 获取总数（带缓存）
func (s *GoodsService) getTotalFromCache(ctx context.Context) (int64, error) {
	cached, err := repository.Redis.Get(ctx, goodsTotalCacheKey).Int64()
	if err == nil {
		return cached, nil
	}

	// 缓存未命中，查询并更新
	var total int64
	s.db.WithContext(ctx).
		Model(&model.SeckillGoods{}).
		Where("status = 1 AND start_time <= ? AND end_time > ?", time.Now(), time.Now()).
		Count(&total)

	repository.SetWithExpire(ctx, goodsTotalCacheKey, total, goodsTotalCacheTTL)
	return total, nil
}
