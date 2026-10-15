package model

type Customer struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Tag    String `json:"tag"`
}

type Item struct {
	SKU string `json:"sku"`
}

// Status is the customer status, which clashes with api.Status.
type Status int

// String clashes with the Pkl String type.
type String string
