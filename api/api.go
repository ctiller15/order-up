// Package api exposes an HTTP handler to handle REST API calls for manipulating
// and retrieving orders
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/levenlabs/order-up/mocks"
	"github.com/levenlabs/order-up/storage"
)

// instance represents an API instance. Typically this is exported but for our
// purposes we don't need to actually expose any methods on it since we only
// return an http.Handler implementation.
type instance struct {
	stor               mocks.StorageInstance
	router             *gin.Engine
	fulfillmentService *http.Client
	chargeService      *http.Client
	mu                 sync.Mutex
}

// Handler returns an implementation of the http.Handler interface that can be
// passed to an http.Server to handle incoming HTTP requests. This accepts
// an interface for the storage.Instance and http.Client's for the 2 dependent
// services. Typically this would accept just a *storage.Instance but the mock
// allows us to separate the api tests from the storage tests.
func Handler(stor mocks.StorageInstance, fulfillmentService, chargeService *http.Client) http.Handler {
	// inst is pointer to a new instance that's holding a new storage.Instance for
	// talking to the underlying database
	inst := &instance{
		stor:               stor,
		router:             gin.Default(),
		fulfillmentService: fulfillmentService,
		chargeService:      chargeService,
	}

	// set up the various REST endpoints that are exposed publicly over HTTP
	// go implicitly binds these functions to inst
	inst.router.GET("/orders", inst.getOrders)
	inst.router.POST("/orders", inst.postOrders)
	inst.router.GET("/orders/:id", inst.getOrder)
	inst.router.POST("/orders/:id/charge", inst.chargeOrder)
	inst.router.POST("/orders/:id/cancel", inst.cancelOrder)
	inst.router.PUT("/orders/:id/fulfill", inst.fulFillOrder)

	// *instance implements the http.Handler interface with the ServeHTTP method
	// below so we can just return inst
	return inst
}

// ServeHTTP implements the http.Handler interface and passes incoming HTTP
// requests to the underlying *gin.Engine
func (i *instance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.router.ServeHTTP(w, r)
}

////////////////////////////////////////////////////////////////////////////////

type getOrdersRes struct {
	Orders []storage.Order `json:"orders"`
}

// getOrders is called by incoming HTTP GET requests to /orders
func (i *instance) getOrders(c *gin.Context) {
	// the context of the request we pass along to every downstream function so we
	// can stop processing if the caller aborts the request and also to ensure that
	// the tracing context is kept throughout the whole request
	ctx := c.Request.Context()

	// get and parse the optional status query parameter from the request
	// this lets you do /orders?status=pending to limit the orders to only those that
	// are currently pending
	var status storage.OrderStatus
	switch c.Query("status") {
	case "pending":
		status = storage.OrderStatusPending
		// the final break is implied if there's no fallthrough keyword
	case "charged":
		status = storage.OrderStatusCharged
	case "fulfilled":
		status = storage.OrderStatusFulfilled
		// Add case for cancelled.
	case "":
		// GetAllOrders accepts a -1 to indicate that all orders should be returned
		status = -1
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown value for status: %v"})
		return
	}

	// pass along the status and get all of the resulting orders from the storage
	// instance
	orders, err := i.stor.GetOrders(ctx, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error getting orders: %v", err)})
		return
	}

	// by default slices are nil and if we return that the resulting JSON would be
	// {"orders":null} which some languages/clients have a problem with
	// instead set it to an empty slice
	if orders == nil {
		orders = []storage.Order{}
	}

	// respond with a success and return the orders
	c.JSON(http.StatusOK, getOrdersRes{
		Orders: orders,
	})
}

////////////////////////////////////////////////////////////////////////////////

// getOrderRes is the result of the GET /orders/:id handler
// you might think its unnecessary for this struct and we should instead just
// return the order itself but this gives us future flexibility to return
// anything else alongside that we can't think of right now
type getOrderRes struct {
	Order storage.Order `json:"order"`
}

// getOrder is called by incoming HTTP GET requests to /orders/:id
func (i *instance) getOrder(c *gin.Context) {
	// the context of the request we pass along to every downstream function so we
	// can stop processing if the caller aborts the request and also to ensure that
	// the tracing context is kept throughout the whole request
	ctx := c.Request.Context()

	// since the path includes a param :id we can get the value for that by calling
	// the Param function
	id := c.Param("id")

	order, err := i.stor.GetOrder(ctx, id)
	if err != nil {
		// if the error is a ErrOrderNotFound error then we return 404 otherwise we
		// return a 500 error
		if errors.Is(err, storage.ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error getting order: %v", err)})
		}
		return
	}

	// respond with a success and return the order
	c.JSON(http.StatusOK, getOrderRes{
		Order: order,
	})
}

////////////////////////////////////////////////////////////////////////////////

// postOrderArgs is the expected body for the POST /orders handler
type postOrderArgs struct {
	CustomerEmail string             `json:"customerEmail"`
	LineItems     []storage.LineItem `json:"lineItems"`
}

// chargeOrderRes is the result of the POST /orders/:id/charge handler
type postOrderRes struct {
	Order storage.Order `json:"order"`
}

// postOrders is called by incoming HTTP POST requests to /orders
func (i *instance) postOrders(c *gin.Context) {
	// the context of the request we pass along to every downstream function so we
	// can stop processing if the caller aborts the request and also to ensure that
	// the tracing context is kept throughout the whole request
	ctx := c.Request.Context()

	// parse the body as JSON into the newOrderArgs struct
	var args postOrderArgs
	err := c.BindJSON(&args)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("error decoding body: %v", err)})
		return
	}

	// do some light validation
	// we could use something like https://pkg.go.dev/gopkg.in/validator.v2
	// so we could set struct tags but since we only do validation in this one
	// spot that feels like overkill
	if !strings.Contains(args.CustomerEmail, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid customerEmail"})
		return
	}
	if len(args.LineItems) < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "an order must contain at least one line item"})
		return
	}

	order := storage.Order{
		CustomerEmail: args.CustomerEmail,
		LineItems:     args.LineItems,
		Status:        storage.OrderStatusPending,
	}
	if order.TotalCents() < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "an order's total cannot be less than 0"})
	}

	id, err := i.stor.InsertOrder(ctx, order)
	if err != nil {
		// if the error is a ErrOrderExists error then we return 409 otherwise we
		// return a 500 error
		if errors.Is(err, storage.ErrOrderExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "order already exists"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error inserting order: %v", err)})
		}
		return
	}
	order.ID = id

	// respond with a success and return the order
	c.JSON(http.StatusCreated, postOrderRes{
		Order: order,
	})
}

////////////////////////////////////////////////////////////////////////////////

// chargeServiceChargeArgs is the expected body for the POST /charge method of
// the charge service
// we could use a map[string]interface{}{} or something else but this makes it
// easier to use in tests and makes the API contract clear
// we could also be importing something from the charge service instead if that
// actually existed
type chargeServiceChargeArgs struct {
	CardToken   string `json:"cardToken"`
	AmountCents int64  `json:"amountCents"`
}

// innerChargeOrder actually does the charging or refunding (negative amount) by
// making at POST request to the charge service
func (i *instance) innerChargeOrder(ctx context.Context, args chargeServiceChargeArgs) error {
	// encode the charge service's charge arguments as JSON so we can POST them to
	// the /charge path on the charge service
	// this method returns a byte slice that we can later pass to the Post message
	// as the body of the POST request
	// there's a package called "bytes" so we call the variable byts
	byts, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("error encoding charge body: %w", err)
	}

	// make a POST request to the /charge endpoint on the charge service
	// the body is JSON but this method accepts a io.Reader so we need to wrap the
	// byte slice in bytes.NewReader which simply reads over the sent byte slice
	i.mu.Lock()
	resp, err := i.chargeService.Post("/charge", "application/json", bytes.NewReader(byts))
	i.mu.Unlock()

	if err != nil {
		return fmt.Errorf("error making charge request: %w", err)
	}
	// we need to make sure we close the body otherwise this will leak memory
	defer resp.Body.Close()
	// /charge creates a new charge so we expect a 201 response, if we didn't get
	// that then we must've errored
	if resp.StatusCode != http.StatusCreated {
		// we opportunistically try to read the body in case it contains an error but
		// if it fails then that's not the end of the world so we ignore the error
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("error charging body: %d %s", resp.StatusCode, body)
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// chargeOrderArgs is the expected body for the POST /orders/:id/charge handler
type chargeOrderArgs struct {
	CardToken string `json:"cardToken"`
}

// chargeOrderRes is the result of the POST /orders/:id/charge handler
type chargeOrderRes struct {
	ChargedCents int64 `json:"chargedCents"`
}

// chargeOrder is called by incoming HTTP POST requests to /orders/:id/charge
func (i *instance) chargeOrder(c *gin.Context) {
	// the context of the request we pass along to every downstream function so we
	// can stop processing if the caller aborts the request and also to ensure that
	// the tracing context is kept throughout the whole request
	ctx := c.Request.Context()

	// parse the body as JSON into the chargeOrderArgs struct
	var args chargeOrderArgs
	err := c.BindJSON(&args)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("error decoding body: %v", err)})
		return
	}

	// since the path includes a param :id we can get the value for that by calling
	// the Param function
	id := c.Param("id")

	fmt.Println("HERE!!!")

	// make a call to the storage instance to get the current state of the order
	// so we can make sure that its ready for charging and get the amount to charge
	order, err := i.stor.GetOrder(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error getting order: %v", err)})
		}
		return
	}

	// Based on the test cases I'm assuming this should error if already charged.
	// Or fulfilled.
	if order.Status == storage.OrderStatusCharged || order.Status == storage.OrderStatusFulfilled {
		c.JSON(http.StatusConflict, gin.H{"error": "order ineligible for charging"})
		return
	}

	// We know that you can charge a negative cents amount, so I'm opting to just
	// Error out if it is explicitly zero, not if it's negative.
	if order.TotalCents() != 0 {
		err = i.innerChargeOrder(ctx, chargeServiceChargeArgs{
			CardToken:   args.CardToken,
			AmountCents: order.TotalCents(),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// in a real-world scenario we would do a two-phase change where we set it to
	// charging ahead of time and then mark it as charged after so we would be able
	// to understand if this was retried that we already tried to charge
	// as it's written if this service crashed before this line then we would've
	// charged the customer and not reflected that on the order but for now we're
	// ignoring this scenario
	err = i.stor.SetOrderStatus(ctx, order.ID, storage.OrderStatusCharged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error updating order to charged: %v", err)})
		return
	}

	// since we successfully charged the order and updated the order status we can
	// return a success to the caller
	c.JSON(http.StatusOK, chargeOrderRes{
		ChargedCents: order.TotalCents(),
	})
}

////////////////////////////////////////////////////////////////////////////////

func (i *instance) refundLineItems(ctx context.Context, lineItems []storage.LineItem, cardToken string) (int64, error) {
	var totalRefund int64

	// Calculate total refund
	for _, lineItem := range lineItems {
		totalRefund += lineItem.PriceCents * lineItem.Quantity
	}

	// Then send to inner charge

	err := i.innerChargeOrder(ctx, chargeServiceChargeArgs{
		CardToken:   cardToken,
		AmountCents: -totalRefund,
	})
	if err != nil {
		return 0, err
	}

	return -totalRefund, nil

}

type cancelOrderArgs struct {
	CardToken string `json:"cardToken"`
}

// chargeOrderRes is the result of the POST /orders/:id/charge handler
type cancelOrderRes struct {
	OrderStatus  string `json:"orderStatus"`
	ChargedCents int64  `json:"chargedCents"`
}

// TODO: cancel args, res, function
// cancelOrder is called by incoming HTTP POST requests to /orders/:id/cancel
func (i *instance) cancelOrder(c *gin.Context) {
	ctx := c.Request.Context()

	// parse the body as JSON into the chargeOrderArgs struct
	var args cancelOrderArgs
	err := c.BindJSON(&args)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("error decoding body: %v", err)})
		return
	}

	id := c.Param("id")

	// Get order
	order, err := i.stor.GetOrder(ctx, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error getting order: %v", err)})
		return
	}

	var refundAmt int64
	// If order is charged
	// Refund charge on line items.
	// Update to cancelled.
	if order.Status == storage.OrderStatusCharged {
		refundAmt, err = i.refundLineItems(ctx, order.LineItems, args.CardToken)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error refunding line items: %v", err)})
			return
		}

		err = i.stor.SetOrderStatus(ctx, id, storage.OrderStatusCancelled)
		if err != nil {
			// At this point it would just be an issue with setting the status. The refund has already occurred.
			// Would likely be good to log internally for a manual fix or handle as part of a retry to set the status.
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error cancelling order: %v", err)})
			return
		}
	} else if order.Status == storage.OrderStatusFulfilled {
		c.JSON(http.StatusConflict, gin.H{"error": "order has already been fulfilled"})
		return
	}

	c.JSON(http.StatusOK, cancelOrderRes{
		OrderStatus:  "cancelled",
		ChargedCents: refundAmt,
	})
}

////////////////////////////////////////////////////////////////////////////////

// fulfillmentServiceFulfillArgs are the arguments for the PUT /fulfill endpoint
// exposed by the fulfillment service
type fulfillmentServiceFulfillArgs struct {
	Description string `json:"description"`
	Quantity    int64  `json:"quantity"`
	OrderID     string `json:"orderID"`
}

// innerChargeOrder actually does the charging or refunding (negative amount) by
// making at POST request to the charge service
func (i *instance) innerFulfillOrder(ctx context.Context, args fulfillmentServiceFulfillArgs) error {
	byts, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("error encoding fulfill body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, "/fulfill", bytes.NewReader(byts))
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.fulfillmentService.Do(req)

	if err != nil {
		return fmt.Errorf("error making fulfillment request: %w", err)
	}
	// we need to make sure we close the body otherwise this will leak memory
	defer resp.Body.Close()
	// /fulfill expects a 200 OK, if we didn't get
	// that then we must've errored
	if resp.StatusCode != http.StatusOK {
		// we opportunistically try to read the body in case it contains an error but
		// if it fails then that's not the end of the world so we ignore the error
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("error fulfilling body: %d %s", resp.StatusCode, body)
	}

	// For the purposes of this exercise we'll assume that a 200 means the entire order was fulfilled.
	// In reality this is more complicated.
	return nil
}

func (i *instance) fulfillOrders(ctx context.Context, orderID string, lineItems []storage.LineItem) (bool, error) {
	// A variable to track if the entire order has been fulfilled.
	for _, item := range lineItems {
		args := fulfillmentServiceFulfillArgs{
			Description: item.Description,
			OrderID:     orderID,
			Quantity:    item.Quantity,
		}

		err := i.innerFulfillOrder(ctx, args)

		if err != nil {
			return false, fmt.Errorf("fulfillOrders: %v", err)
		}

		// Assume the fulfillment service handles the logic behind saying if a given
		// Line item has been fulfilled or not,
		// based off of the quantity.
	}

	// Made it to the end, has been fulfilled on the order.
	return true, nil
}

// TODO: fulfill args, res, function
func (i *instance) fulFillOrder(c *gin.Context) {
	ctx := c.Request.Context()

	id := c.Param("id")

	// Get order
	order, err := i.stor.GetOrder(ctx, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error getting order: %v", err)})
		return
	}

	if order.Status != storage.OrderStatusCharged {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order cannot be fulfilled, order has not been charged"})
		return
	} else {
		allFulfilled, err := i.fulfillOrders(ctx, id, order.LineItems)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error fulfilling line items: %v", err)})
			return
		}

		if allFulfilled {
			// If allFulfilled is true, update status.
			err = i.stor.SetOrderStatus(ctx, id, storage.OrderStatusFulfilled)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error updating order to fulfilled: %v", err)})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"fulfilled": "true"})
		return
	}
}
