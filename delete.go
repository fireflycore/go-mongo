package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// DeleteById 按id删除单条文档，并返回 driver 的 DeleteResult。
func Delete(ctx context.Context, collection *mongo.Collection, id string) (*mongo.DeleteResult, error) {
	return collection.DeleteOne(ctx, bson.D{
		{Key: "_id", Value: id},
	})
}

// DeleteManyByIds 按id列表批量删除文档，并返回 driver 的 DeleteResult。
func DeleteManyByIds(ctx context.Context, collection *mongo.Collection, ids []string) (*mongo.DeleteResult, error) {
	return collection.DeleteMany(ctx, bson.D{
		{Key: "_id", Value: bson.D{
			{Key: "$in", Value: ids},
		}},
	})
}

// SoftDeleteById 软删除单条文档：写入 updated_at 与 deleted_at，并返回 UpdateResult。
func SoftDeleteById(ctx context.Context, collection *mongo.Collection, id string) (*mongo.UpdateResult, error) {
	timer := time.Now().UTC()

	return collection.UpdateOne(ctx, bson.D{
		{Key: "_id", Value: id},
	}, bson.D{
		{Key: "$set", Value: bson.M{
			"updated_at": timer,
			"deleted_at": timer,
		}},
	})
}

// SoftDeleteManyByIds 软删除多条文档：批量写入 updated_at 与 deleted_at，并返回 UpdateResult。
func SoftDeleteManyByIds(ctx context.Context, collection *mongo.Collection, ids []string) (*mongo.UpdateResult, error) {
	timer := time.Now().UTC()

	return collection.UpdateMany(ctx, bson.D{
		{Key: "_id", Value: bson.D{
			{Key: "$in", Value: ids},
		}},
	}, bson.D{
		{Key: "$set", Value: bson.M{
			"updated_at": timer,
			"deleted_at": timer,
		}},
	})
}
