package httpclient

import (
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// Middleware wraps a transport, to run code before and after the requests: add headers,
// retry, record...
type Middleware interface {
	Wrap(next http.RoundTripper) http.RoundTripper
}

// MiddlewareFunc is a func(http.RoundTripper) http.RoundTripper [Middleware].
type MiddlewareFunc func(next http.RoundTripper) http.RoundTripper

// Wrap calls f(next).
func (f MiddlewareFunc) Wrap(next http.RoundTripper) http.RoundTripper {
	return f(next)
}

// RoundTripperFunc is a function [http.RoundTripper], to write middlewares concisely:
//
//	httpclient.MiddlewareFunc(func(next http.RoundTripper) http.RoundTripper {
//		return httpclient.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
//			r = r.Clone(r.Context())
//			r.Header.Set("Authorization", "Bearer "+token)
//
//			return next.RoundTrip(r)
//		})
//	})
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls f(r).
func (f RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// excluded reports whether the request URL matches one of the patterns.
func excluded(patterns []*regexp.Regexp, r *http.Request) bool {
	if len(patterns) == 0 {
		return false
	}

	target := safeURL(r.URL)

	for _, pattern := range patterns {
		if pattern.MatchString(target) {
			return true
		}
	}

	return false
}

// safeURL returns the URL without its user info, query and fragment, which can hold secrets.
func safeURL(u *url.URL) string {
	safe := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}

	return safe.String()
}

// newLogMiddleware logs every request not excluded: at error level for transport errors and
// 5xx responses, and info level otherwise.
func newLogMiddleware(logger *slog.Logger, exclude []*regexp.Regexp) Middleware {
	return MiddlewareFunc(func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if excluded(exclude, r) {
				return next.RoundTrip(r)
			}

			start := time.Now()
			response, err := next.RoundTrip(r)

			attributes := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("url", safeURL(r.URL)),
			}

			level := slog.LevelInfo

			if err != nil {
				level = slog.LevelError
				attributes = append(attributes, slog.Duration("duration", time.Since(start)), slog.String("error", err.Error()))
			} else {
				if response.StatusCode >= http.StatusInternalServerError {
					level = slog.LevelError
				}

				attributes = append(attributes, slog.Int("status", response.StatusCode), slog.Duration("duration", time.Since(start)))
			}

			// The request context holds the client span: the log is correlated with it.
			logger.LogAttrs(r.Context(), level, "http client request", attributes...)

			return response, err
		})
	})
}
