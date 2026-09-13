// pctx.go — Typed context keys for intra-process proxy metadata.
// Single-user mode: only trimmedTokens context key is needed.
package proxy

import (
	"context"
	"net/http"
)

type ctxKey int

const (
	ctxTrimmedTokens ctxKey = iota // token count saved by trimmer (int64)
	ctxConvID                      // Glass session key (string)
	ctxGlassResult                 // glass.ProcessResult
)

func withTrimmedTokens(r *http.Request, n int64) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxTrimmedTokens, n))
}

func trimmedTokensFrom(ctx context.Context) int64 {
	if v, ok := ctx.Value(ctxTrimmedTokens).(int64); ok {
		return v
	}
	return 0
}

func withConvID(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxConvID, id))
}

func convIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxConvID).(string); ok {
		return v
	}
	return ""
}

func withGlassResult(r *http.Request, res interface{}) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxGlassResult, res))
}

func glassResultFrom(ctx context.Context) interface{} {
	return ctx.Value(ctxGlassResult)
}
