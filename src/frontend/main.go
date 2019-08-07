// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	// Added Instana
	instana "github.com/LutzLange/go-sensor"
	ot "github.com/opentracing/opentracing-go"
	// "github.com/opentracing/opentracing-go/ext"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
)

const (
	port            = "8080"
	defaultCurrency = "USD"
	cookieMaxAge    = 60 * 60 * 48

	cookiePrefix    = "shop_"
	cookieSessionID = cookiePrefix + "session-id"
	cookieCurrency  = cookiePrefix + "currency"

	service = "frontend"
)

var (
	whitelistedCurrencies = map[string]bool{
		"USD": true,
		"EUR": true,
		"CAD": true,
		"JPY": true,
		"GBP": true,
		"TRY": true}
	/*
		tracer = instana.NewTracerWithOptions(&instana.Options{
			Service:  service,
			LogLevel: instana.Debug})
	*/
	// add Instana Tracing
	sensor = instana.NewSensor(service)
	tracer = sensor.Tracer()
	//var sensor = &instana.Sensor{tracer}
)

type ctxKeySessionID struct{}

type frontendServer struct {
	productCatalogSvcAddr string
	productCatalogSvcConn *grpc.ClientConn

	currencySvcAddr string
	currencySvcConn *grpc.ClientConn

	cartSvcAddr string
	cartSvcConn *grpc.ClientConn

	recommendationSvcAddr string
	recommendationSvcConn *grpc.ClientConn

	checkoutSvcAddr string
	checkoutSvcConn *grpc.ClientConn

	shippingSvcAddr string
	shippingSvcConn *grpc.ClientConn

	adSvcAddr string
	adSvcConn *grpc.ClientConn
}

func main() {
	ctx := context.Background()
	log := logrus.New()
	log.Level = logrus.DebugLevel
	log.Formatter = &logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "severity",
			logrus.FieldKeyMsg:   "message",
		},
		TimestampFormat: time.RFC3339Nano,
	}
	log.Out = os.Stdout

	//go initProfiling(log, "frontend", "1.0.0")
	//go initTracing(log)

	ot.InitGlobalTracer(tracer)

	srvPort := port
	if os.Getenv("PORT") != "" {
		srvPort = os.Getenv("PORT")
	}
	addr := os.Getenv("LISTEN_ADDR")
	svc := new(frontendServer)
	mustMapEnv(&svc.productCatalogSvcAddr, "PRODUCT_CATALOG_SERVICE_ADDR")
	mustMapEnv(&svc.currencySvcAddr, "CURRENCY_SERVICE_ADDR")
	mustMapEnv(&svc.cartSvcAddr, "CART_SERVICE_ADDR")
	mustMapEnv(&svc.recommendationSvcAddr, "RECOMMENDATION_SERVICE_ADDR")
	mustMapEnv(&svc.checkoutSvcAddr, "CHECKOUT_SERVICE_ADDR")
	mustMapEnv(&svc.shippingSvcAddr, "SHIPPING_SERVICE_ADDR")
	mustMapEnv(&svc.adSvcAddr, "AD_SERVICE_ADDR")

	mustConnGRPC(ctx, &svc.currencySvcConn, svc.currencySvcAddr)
	mustConnGRPC(ctx, &svc.productCatalogSvcConn, svc.productCatalogSvcAddr)
	mustConnGRPC(ctx, &svc.cartSvcConn, svc.cartSvcAddr)
	mustConnGRPC(ctx, &svc.recommendationSvcConn, svc.recommendationSvcAddr)
	mustConnGRPC(ctx, &svc.shippingSvcConn, svc.shippingSvcAddr)
	mustConnGRPC(ctx, &svc.checkoutSvcConn, svc.checkoutSvcAddr)
	mustConnGRPC(ctx, &svc.adSvcConn, svc.adSvcAddr)

	r := mux.NewRouter()
	r.HandleFunc("/", sensor.TracingHandler("homeHandler", svc.homeHandler)).Methods(http.MethodGet, http.MethodHead)
	r.HandleFunc("/product/{id}", sensor.TracingHandler("productHandler", svc.productHandler)).Methods(http.MethodGet, http.MethodHead)
	r.HandleFunc("/cart", sensor.TracingHandler("viewCartHandler", svc.viewCartHandler)).Methods(http.MethodGet, http.MethodHead)
	r.HandleFunc("/cart", sensor.TracingHandler("addToCartHandler", svc.addToCartHandler)).Methods(http.MethodPost)
	r.HandleFunc("/cart/empty", sensor.TracingHandler("emptyCartHandler", svc.emptyCartHandler)).Methods(http.MethodPost)
	r.HandleFunc("/setCurrency", sensor.TracingHandler("setCurrencyHandler", svc.setCurrencyHandler)).Methods(http.MethodPost)
	r.HandleFunc("/logout", sensor.TracingHandler("logoutHandler", svc.logoutHandler)).Methods(http.MethodGet)
	r.HandleFunc("/cart/checkout", sensor.TracingHandler("placeOrderHandler", svc.placeOrderHandler)).Methods(http.MethodPost)
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	r.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "User-agent: *\nDisallow: /") })
	r.HandleFunc("/_healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })

	var handler http.Handler = r
	handler = &logHandler{log: log, next: handler} // add logging
	handler = ensureSessionID(handler)             // add session ID
	//handler = &ochttp.Handler{                     // add opencensus instrumentation
	//	Handler:     handler,
	//	Propagation: &b3.HTTPFormat{}}

	log.Infof("starting server on " + addr + ":" + srvPort)
	log.Fatal(http.ListenAndServe(addr+":"+srvPort, handler))
}

func mustMapEnv(target *string, envKey string) {
	v := os.Getenv(envKey)
	if v == "" {
		panic(fmt.Sprintf("environment variable %q not set", envKey))
	}
	*target = v
}

func mustConnGRPC(ctx context.Context, conn **grpc.ClientConn, addr string) {
	var err error

	/*
		Define a Decorator Function to set rpc.call Tags on all traces
		type SpanDecoratorFunc func(
				  span opentracing.Span,
				  method string,
				  req, resp interface{},
				  grpcError error)
	*/
	decorator := func(
		span ot.Span,
		method string,
		req, resp interface{},
		grpcError error) {
		span.SetTag("rpc.call", method)
	}

	// create the otgrpc.Options for use below
	rpcdecor := otgrpc.SpanDecorator(decorator)

	*conn, err = grpc.DialContext(ctx, addr,
		grpc.WithInsecure(),
		grpc.WithTimeout(time.Second*3),
		grpc.WithUnaryInterceptor(otgrpc.OpenTracingClientInterceptor(tracer, rpcdecor)),
		grpc.WithStreamInterceptor(otgrpc.OpenTracingStreamClientInterceptor(tracer, rpcdecor)))
	if err != nil {
		panic(errors.Wrapf(err, "grpc: failed to connect %s", addr))
	}
}
