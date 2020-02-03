package discovery

import (
	"context"
	"fmt"
	"strings"
)

type Resolver struct {
	s []Source
}

type Source interface {
	Lookup(addr string) (string, bool)
}

type Starter interface {
	Start(ctx context.Context) error
}

func (r *Resolver) Register(s Source) {
	r.s = append(r.s, s)
}

func (r *Resolver) Start(ctx context.Context) {
	t := TraceFromCtx(ctx)
	for _, s := range r.s {
		if s, ok := s.(Starter); ok {
			if err := s.Start(ctx); err != nil {
				if t.OnWarning != nil {
					t.OnWarning(fmt.Sprintf("%T: %v", s, err))
				}
			}
		}
	}
}

func (r *Resolver) Lookup(addr string) string {
	addr = strings.ToLower(addr)
	for _, s := range r.s {
		if name, found := s.Lookup(addr); found {
			return name
		}
	}
	return ""
}
