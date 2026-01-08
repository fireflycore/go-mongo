package mongo

import "github.com/google/uuid"

// NewUUIDv7 生成一个 UUIDv7 字符串
func NewUUIDv7() string {
	u, e := uuid.NewV7()
	if e != nil {
		return NewUUIDv7()
	}
	return u.String()
}
