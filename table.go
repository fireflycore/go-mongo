package mongo

import (
	"time"
)

// Table 为通用表结构字段集合（UUID_V7 + 时间戳 + 软删除）。
type Table struct {
	Id        string     `json:"id" bson:"_id"`
	CreatedAt time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" bson:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at" bson:"deleted_at,omitempty"`
}

// BeforeInsert 为新记录初始化主键与时间字段。
func (t *Table) BeforeInsert() {
	t.Id = NewUUIDv7()
	timer := time.Now().UTC()
	t.CreatedAt = timer
	t.UpdatedAt = timer
	t.DeletedAt = nil
}

// BeforeUpdate 在更新前刷新 UpdatedAt 字段。
func (t *Table) BeforeUpdate() {
	t.UpdatedAt = time.Now().UTC()
}
