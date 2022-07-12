package mongo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/storage"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	/*
		DefaultDB and DefaultCollection refere to the database and collection initiated by docker/mongo/initdb.js file
	*/
	DefaultDB                = "admin-server-db"
	DefaultCollection        = "logs"
	DefaultConnectionTimeout = 10 * time.Second
)

type MongoStorage struct {
	dbClient   *mongo.Client
	collection *mongo.Collection
}

func NewMongoStorage(uri string) (storage.Storage, error) {
	client, err := mongo.NewClient(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultConnectionTimeout)
	defer cancel()

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

func (s *MongoStorage) StoreLog(log storage.Log) error {

	ctx, cancel := context.WithTimeout(context.Background(), DefaultConnectionTimeout)
	defer cancel()

	_, err := s.collection.InsertOne(ctx, log)
	if err != nil {
		// for better debugging
		fmt.Fprintf(os.Stderr, "Error while inserting into the db: %v", err)
		return storage.ErrInsert
	}
	return nil
}

func (s *MongoStorage) GetLogs(query storage.Query) (*storage.Result, error) {

	q := toMongoQuery(query)

	//get the count of the logs
	ctx, cancel := context.WithTimeout(context.Background(), DefaultConnectionTimeout)
	defer cancel()
	count, err := s.collection.CountDocuments(ctx, q)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while performing count query: %v", err)
		return nil, storage.ErrQuery
	}

	opts := options.Find()
	opts.SetSkip(int64(int(query.PageSize) * int(query.Page)))
	opts.SetLimit(int64(query.PageSize))

	ctx, cancel = context.WithTimeout(context.Background(), DefaultConnectionTimeout)
	defer cancel()
	cur, err := s.collection.Find(ctx, q, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while querying logs from db: %v", err)
		return nil, storage.ErrQuery
	}

	var logs []storage.Log
	ctx, cancel = context.WithTimeout(context.Background(), DefaultConnectionTimeout)
	defer cancel()
	err = cur.All(ctx, &logs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while reading query result from db: %v", err)
		return nil, storage.ErrQuery
	}

	return &storage.Result{
		Logs:     logs,
		Count:    uint64(count),
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func (s *MongoStorage) Close() error {
	return s.dbClient.Disconnect(context.Background())
}
