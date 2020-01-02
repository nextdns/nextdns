package host

import "github.com/nextdns/nextdns/host/service"

// InitType returns the type of the host service init.
func InitType() string {
	if srv, err := NewService(service.Config{}); err == nil {
		return srv.Type()
	}
	return "unknown"
}
