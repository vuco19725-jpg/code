package model

import (
	"time"
)

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Phone     string    `gorm:"size:11;uniqueIndex;not null" json:"phone"`
	Password  string    `gorm:"size:128;not null" json:"-"`
	Role      string    `gorm:"size:16;default:user" json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type SeckillGoods struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128;not null" json:"name"`
	Price     float64   `gorm:"type:decimal(10,2);not null" json:"price"`
	Stock     int       `gorm:"not null" json:"stock"`
	StartTime time.Time `gorm:"not null;index:idx_time" json:"start_time"`
	EndTime   time.Time `gorm:"not null;index:idx_time" json:"end_time"`
	Status    int       `gorm:"default:1;index" json:"status"` // 1:上架 0:下架
	Version   int       `gorm:"default:0" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type Order struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrderNo   string    `gorm:"uniqueIndex;size:64;not null" json:"order_no"`
	UserID    uint      `gorm:"uniqueIndex:idx_user_sku;not null" json:"user_id"`
	SkuID     uint      `gorm:"uniqueIndex:idx_user_sku;not null" json:"sku_id"`
	Status    int       `gorm:"default:0" json:"status"` // 0:待支付 1:已支付 2:已取消
	CreatedAt time.Time `json:"created_at"`
}

const (
	OrderStatusPending   = 0
	OrderStatusPaid      = 1
	OrderStatusCancelled = 2
)

type OperationLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Action    string    `gorm:"size:64" json:"action"`
	Target    string    `gorm:"size:64" json:"target"`
	Detail    string    `gorm:"type:text" json:"detail"`
	IP        string    `gorm:"size:32" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}
