package scope

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// WithTimerRange 向过滤条件追加 created_at 时间范围（start/end 为 time.DateTime 格式）。
func WithTimerRange(option bson.D, start, end string) {
	if len(start) != 0 && len(end) != 0 {
		startTime, err := time.Parse(time.DateTime, start)
		if err != nil {
			return
		}

		endTime, err := time.Parse(time.DateTime, end)
		if err != nil {
			return
		}

		option = append(option, bson.E{
			Key: "created_at",
			Value: bson.D{
				{Key: "$gte", Value: startTime},
				{Key: "$lte", Value: endTime},
			},
		})
	}
}
