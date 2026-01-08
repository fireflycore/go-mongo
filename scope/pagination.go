package mongo

import (
	"go.mongodb.org/mongo-driver/mongo/options"
)

// WithPagination 为 FindOptions 设置分页参数（page 从 1 开始）。
func WithPagination(option *options.FindOptions, page, size uint64) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 5
	}
	if size > 100 {
		size = 100
	}

	option.SetLimit(int64(size))
	option.SetSkip(int64((page - 1) * size))
}
