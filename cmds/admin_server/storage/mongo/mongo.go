package mongo

import (
	"context"
	"fmt"
	"strings"

	"github.com/linuxboot/contest/cmds/admin_server/storage"
	"github.com/linuxboot/contest/pkg/xcontext"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	/*
		DefaultDB and DefaultCollection refere to the database and collection initiated by docker/mongo/initdb.js file
	*/
	DefaultDB         = "admin-server-db"
	DefaultCollection = "logs"
)

type MongoStorage struct {
	dbClient   *mongo.Client
	collection *mongo.Collection
}

func NewMongoStorage(ctx xcontext.Context, uri string) (storage.Storage, error) {
	client, err := mongo.NewClient(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	err = client.Connect(ctx)
	if err != nil {
		return nil, err
	}

	// check that the server is alive
	err = client.Ping(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("Err while pinging mongo server: %w", err)
	}

	collection := client.Database(DefaultDB).Collection(DefaultCollection)

	return &MongoStorage{
		dbClient:   client,
		collection: collection,
	}, nil
}

func toMongoQuery(query storage.Query) bson.D {
	q := bson.D{}

	if query.JobID != nil {
		q = append(q, bson.E{
			Key:   "jobid",
			Value: *query.JobID,
		})
	}

	if query.Text != nil {
		q = append(q, bson.E{
			Key: "logdata",
			Value: bson.M{
				"$regex": primitive.Regex{Pattern: *query.Text, Options: "ig"},
			},
		})
	}

	if query.StartDate != nil && query.EndDate != nil {
		q = append(q, bson.E{
			Key: "date",
			Value: bson.M{
				"$gte": primitive.NewDateTimeFromTime(*query.StartDate),
				"$lte": primitive.NewDateTimeFromTime(*query.EndDate),
			},
		})
	} else if query.StartDate != nil {
		q = append(q, bson.E{
			Key:   "date",
			Value: bson.M{"$gte": primitive.NewDateTimeFromTime(*query.StartDate)},
		})
	} else if query.EndDate != nil {
		q = append(q, bson.E{
			Key:   "date",
			Value: bson.M{"$lte": primitive.NewDateTimeFromTime(*query.EndDate)},
		})
	}

	if query.LogLevel != nil && *query.LogLevel != "" {
		levels := strings.Split(*query.LogLevel, ",")
		q = append(q,
			bson.E{
				Key: "loglevel",
				Value: bson.M{
					"$in": levels,
				},
			},
		)
	}

	return q
}

func (s *MongoStorage) StoreLog(ctx xcontext.Context, log storage.Log) error {
	_, err := s.collection.InsertOne(ctx, log)
	if err != nil {
		// for better debugging
		ctx.Errorf("Error while inserting into the db: %v", err)
		return storage.ErrInsert
	}
	return nil
}

func (s *MongoStorage) GetLogs(ctx xcontext.Context, query storage.Query) (*storage.Result, error) {
	q := toMongoQuery(query)
	//get the count of the logs
	count, err := s.collection.CountDocuments(ctx, q)
	if err != nil {
		ctx.Errorf("Error while performing count query: %v", err)
		return nil, storage.ErrQuery
	}

	opts := options.Find()
	opts.SetSkip(int64(int(query.PageSize) * int(query.Page)))
	opts.SetLimit(int64(query.PageSize))

	cur, err := s.collection.Find(ctx, q, opts)
	if err != nil {
		ctx.Errorf("Error while querying logs from db: %v", err)
		return nil, storage.ErrQuery
	}

	var logs []storage.Log
	err = cur.All(ctx, &logs)
	if err != nil {
		ctx.Errorf("Error while reading query result from db: %v", err)
		return nil, storage.ErrQuery
	}

	return &storage.Result{
		Logs:     logs,
		Count:    uint64(count),
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func (s *MongoStorage) Close(ctx xcontext.Context) error {
	return s.dbClient.Disconnect(ctx)
}
