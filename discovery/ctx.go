package discovery

import "context"

type Trace struct {
	OnWarning  func(string)
	OnDiscover func(addr, host, source string)
}

var key struct{}

func WithTrace(ctx context.Context, t Trace) context.Context {
	return context.WithValue(ctx, key, t)
}

func TraceFromCtx(ctx context.Context) Trace {
	t, _ := ctx.Value(key).(Trace)
	return t
}
