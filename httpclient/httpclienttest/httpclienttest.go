// Package httpclienttest serves the requests of the httpclient clients with an [http.Handler],
// in memory: tests of an application calling an HTTP API need no network, and keep the whole
// client chain (middlewares, instrumentation, logs).
//
//	coretest.Start(t, app,
//		httpclienttest.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			fmt.Fprint(w, `{"temperature": 21}`)
//		})),
//		fx.Populate(&weatherClient),
//	)
package httpclienttest

import (
	"net/http"
	"net/http/httptest"

	"go.uber.org/fx"

	"github.com/go-oryn/oryn/httpclient"
)

// RemoteAddr is the client address seen by the handler.
const RemoteAddr = "192.0.2.1:1234"

// Serve replaces the [httpclient.BaseTransport] of the application with [Transport]: every client
// of the httpclient factory sends its requests to the handler, in memory.
func Serve(handler http.Handler) fx.Option {
	return fx.Replace(httpclient.BaseTransport{RoundTripper: Transport(handler)})
}

// Transport returns an [http.RoundTripper] serving the requests with the handler, in memory.
// The handler gets a server request, like from a real server; the response is buffered.
func Transport(handler http.Handler) http.RoundTripper {
	return httpclient.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}

		server := r.Clone(r.Context())
		server.RequestURI = r.URL.RequestURI()
		server.RemoteAddr = RemoteAddr

		if server.Body == nil {
			server.Body = http.NoBody
		}

		if server.Host == "" {
			server.Host = r.URL.Host
		}

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, server)

		response := recorder.Result()
		response.Request = r

		return response, nil
	})
}
