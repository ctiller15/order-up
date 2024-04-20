package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	// ErrOrderNotFound is returned when the specified order cannot be found
	ErrOrderNotFound = errors.New("order not found")

	// ErrOrderExists is returned when a new order is being inserted but an order
	// with the same ID already exists
	ErrOrderExists = errors.New("order already exists")
)

////////////////////////////////////////////////////////////////////////////////

// GetOrder should return the order with the given ID. If that ID isn't found then
// the special ErrOrderNotFound error should be returned.
func (i *Instance) GetOrder(ctx context.Context, id string) (Order, error) {
	// TODO: get order from DB based on the id
	return Order{}, errors.New("unimplemented")
}

////////////////////////////////////////////////////////////////////////////////

// GetOrders should return all orders with the given status. If status is the
// special -1 value then it should return all orders regardless of their status.
func (i *Instance) GetOrders(ctx context.Context, status OrderStatus) ([]Order, error) {
	// TODO: get orders from DB based based on the status sent, unless status is -1
	return nil, errors.New("unimplemented")
}

////////////////////////////////////////////////////////////////////////////////

// SetOrderStatus should update the order with the given ID and set the status
// field. If that ID isn't found then the special ErrOrderNotFound error should
// be returned.
func (i *Instance) SetOrderStatus(ctx context.Context, id string, status OrderStatus) error {
	// TODO: update the order's status field to status for the id
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// InsertOrder should fill in the order's ID with a unique identifier if it's not
// already set and then insert it into the database. It should return the order's
// ID. If the order already exists then ErrOrderExists should be returned.
func (i *Instance) InsertOrder(ctx context.Context, order Order) (string, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))

	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			panic(err)
		}
	}()

	collection := client.Database("order-up-tests").Collection("orders")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if order.ID != "" {
		opts := options.Update().SetUpsert(true)
		filter := bson.D{{Key: "_id", Value: order.ID}}
		fmt.Printf("Order id: %s\n", order.ID)
		res, err := collection.UpdateOne(ctx, filter, bson.D{
			{Key: "_id", Value: order.ID},
			{Key: "customerEmail", Value: order.CustomerEmail},
			{Key: "status", Value: order.Status},
			{Key: "lineItems", Value: order.LineItems}}, opts)

		_ = res
		_ = err
	} else {
		fmt.Println("No order id found. Generating.")
	}
	res, err := collection.InsertOne(ctx, bson.D{{Key: "name", Value: "pi"}, {Key: "value", Value: 3.14159}})

	fmt.Println(res)
	// TODO: if the order's ID field is empty, generate a random ID, then insert
	// into the database
	return "", errors.New("unimplemented")
}
