package api

import (
	"example.com/shop/model"
	m2 "example.com/shop/model"
)

// Order is placed by a customer.
type Order struct {
	ID       string         `json:"id"`
	Customer model.Customer `json:"customer"`
	Items    []m2.Item      `json:"items"`
	Status   Status         `json:"status"`
}

// Status is the order status.
type Status string

const (
	Pending Status = "pending"
	Shipped Status = "shipped"
)
