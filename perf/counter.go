package perf

import "sync/atomic"

// IntValue represents an integer counter
type IntValue struct {
	val int64
}

// Get returns the counter's value
func (v *IntValue) Get() int64 {
	return v.val
}

// Add increments the val by x
func (v *IntValue) Add(x int64) {
	atomic.AddInt64(&v.val, x)
}

// Set overwrites the val by x
func (v *IntValue) Set(x int64) {
	atomic.StoreInt64(&v.val, x)
}
