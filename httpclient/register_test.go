package httpclient_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/httpclient"
	"github.com/go-oryn/oryn/httpclient/httpclienttest"
)

// headerMiddleware appends its name to the X-Order header of the requests.
type headerMiddleware string

func (m headerMiddleware) Wrap(next http.RoundTripper) http.RoundTripper {
	return httpclient.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Add("X-Order", string(m))

		return next.RoundTrip(r)
	})
}

// newConfigMiddleware is a middleware constructor with a dependency.
func newConfigMiddleware(cfg *config.Config) httpclient.Middleware {
	return headerMiddleware(cfg.GetString("test_name"))
}

func TestAsMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("applied in registration order", func(t *testing.T) {
		t.Parallel()

		var (
			factory *httpclient.Factory
			order   []string
		)

		coretest.Start(t,
			newApp(map[string]any{"test_name": "constructor"},
				httpclient.AsMiddleware(headerMiddleware("first"), func(next http.RoundTripper) http.RoundTripper {
					return headerMiddleware("func").Wrap(next)
				}),
				httpclient.AsMiddleware(newConfigMiddleware),
			),
			httpclienttest.Serve(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				order = r.Header.Values("X-Order")
			})),
			fx.Populate(&factory),
		)

		client := factory.NewClient(httpclient.WithMiddlewares(headerMiddleware("option")))
		get(t, client, "https://api.example.com/")

		assert.Equal(t, []string{"first", "func", "constructor", "option"}, order)
	})

	t.Run("invalid registrations", func(t *testing.T) {
		t.Parallel()

		for name, middleware := range map[string]any{
			"nil":         nil,
			"unsupported": "test_value",
			"constructor": func() string { return "test_value" },
		} {
			err := startError(t, newApp(nil, httpclient.AsMiddleware(middleware)))
			require.Error(t, err, name)
			assert.True(t, strings.Contains(err.Error(), "cannot register http client middleware"), name)
		}
	})
}
