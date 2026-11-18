package main

import (
	"net/http"
	"time"
)

var testTime = time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

type observeTransport struct {
	base   http.RoundTripper
	before func()
}

func (o observeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	o.before()
	return o.base.RoundTrip(req)
}
