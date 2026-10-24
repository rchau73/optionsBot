package tests

import "testing"

// The phone-width check (#309, frontend/e2e/) measures the Working orders
// table on demo data: the demo must always serve a working order.
func TestMonitorDemo_ServesAWorkingOrder(t *testing.T) {
	get := demoAPI(t)
	var orders struct {
		Pending []struct {
			Repair bool `json:"repair"`
			Legs   []struct {
				Instrument string  `json:"instrument"`
				Qty        float64 `json:"qty"`
			} `json:"legs"`
		} `json:"pending"`
	}
	get("/api/orders", &orders)
	if len(orders.Pending) != 1 || len(orders.Pending[0].Legs) != 1 {
		t.Fatalf("pending %+v, want one working order with one leg", orders.Pending)
	}
	if l := orders.Pending[0].Legs[0]; l.Instrument == "" || l.Qty <= 0 {
		t.Errorf("working leg %+v: want an instrument and a size", l)
	}
	var status struct {
		Pending int `json:"pending"`
	}
	get("/api/status", &status)
	if status.Pending != 1 {
		t.Errorf("status pending = %d, want 1", status.Pending)
	}
}
