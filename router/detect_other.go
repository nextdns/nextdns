// +build !linux

package router

func detectRouter() (Router, error) {
	return nil, ErrRouterNotSupported
}
