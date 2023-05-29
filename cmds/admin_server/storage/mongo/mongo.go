package mongo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/storage"
	"github.com/linuxboot/contest/pkg/logging"

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

func NewMongoStorage(ctx context.Context, uri string) (*MongoStorage, error) {
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
			Key:   "job_id",
			Value: *query.JobID,
		})
	}

	if query.Text != nil {
		q = append(q, bson.E{
			Key: "log_data",
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
				Key: "log_level",
				Value: bson.M{
					"$in": levels,
				},
			},
		)
	}

	return q
}

func (s *MongoStorage) StoreLogs(ctx context.Context, logs []storage.Log) error {
	var mongoLogs []interface{}
	for _, log := range logs {
		mongoLogs = append(mongoLogs, toMongoLog(&log))
	}
	_, err := s.collection.InsertMany(ctx, mongoLogs)
	if err != nil {
		logging.Errorf(ctx, "Error while inserting a batch of logs: %v", err)
		return storage.ErrInsert
	}
	return nil
}

func (s *MongoStorage) GetLogs(ctx context.Context, query storage.Query) (*storage.Result, error) {
	q := toMongoQuery(query)
	//get the count of the logs
	count, err := s.collection.CountDocuments(ctx, q)
	if err != nil {
		logging.Errorf(ctx, "Error while performing count query: %v", err)
		return nil, storage.ErrQuery
	}

	opts := options.Find()
	opts.SetSkip(int64(int(query.PageSize) * int(query.Page)))
	opts.SetLimit(int64(query.PageSize))

	cur, err := s.collection.Find(ctx, q, opts)
	if err != nil {
		logging.Errorf(ctx, "Error while querying logs from db: %v", err)
		return nil, storage.ErrQuery
	}

	var logs []Log
	err = cur.All(ctx, &logs)
	if err != nil {
		logging.Errorf(ctx, "Error while reading query result from db: %v", err)
		return nil, storage.ErrQuery
	}
	// convert to storage logs
	storageLogs := make([]storage.Log, 0, len(logs))
	for _, log := range logs {
		storageLogs = append(storageLogs, log.toStorageLog())
	}

	return &storage.Result{
		Logs:     storageLogs,
		Count:    uint64(count),
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func (s *MongoStorage) Close(ctx context.Context) error {
	return s.dbClient.Disconnect(ctx)
}

type Log struct {
	JobID    uint64    `bson:"job_id"`
	LogData  string    `bson:"log_data"`
	Date     time.Time `bson:"date"`
	LogLevel string    `bson:"log_level"`
}

func (l *Log) toStorageLog() storage.Log {
	return storage.Log{
		JobID:    l.JobID,
		LogData:  l.LogData,
		Date:     l.Date,
		LogLevel: l.LogLevel,
	}
}

func toMongoLog(l *storage.Log) Log {
	return Log{
		JobID:    l.JobID,
		LogData:  l.LogData,
		Date:     l.Date,
		LogLevel: l.LogLevel,
	}
}
