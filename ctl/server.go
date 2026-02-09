package ctl

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
)

// Server provides a bi-directional event stream with clients on top of named
// pipes.
type Server struct {
	Addr string

	OnConnect    func(c net.Conn)
	OnDisconnect func(c net.Conn)
	OnEvent      func(c net.Conn, e Event)

	// ErrorLog specifies an optional log function for errors. If not set,
	// errors are not reported.
	ErrorLog func(error)

	mu      sync.Mutex
	cmds    map[string]func(data interface{}) interface{}
	clients []net.Conn
	closer  io.Closer
}

// Event represents an event either received from or sent to a client.
type Event struct {
	Name  string      `json:"name"`
	Data  interface{} `json:"data"`
	Reply bool        `json:"reply"`
}

func (e Event) Bytes() []byte {
	b, err := json.Marshal(e)
	if err != nil {
		return nil
	}
	b = append(b, '\n')
	return b
}

func (s *Server) Start() error {
	ln, err := listen(s.Addr)
	if err != nil {
		return err
	}
	s.closer = ln
	go s.run(ln)
	return nil
}

func (s *Server) Command(cmd string, h func(data interface{}) interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmds == nil {
		s.cmds = map[string]func(data interface{}) interface{}{}
	}
	s.cmds[cmd] = h
}

// Broadcast broadcasts e to all connected clients.
func (s *Server) Broadcast(e Event) error {
	b := e.Bytes()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		if _, err := c.Write(b); err != nil {
			s.logErr(fmt.Errorf("write event: %v", err))
		}
	}
	return nil
}

func (s *Server) run(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			s.logErr(err)
			continue
		}
		go s.handleEvents(c)
	}
}

func (s *Server) handleEvents(c net.Conn) {
	if s.OnConnect != nil {
		s.OnConnect(c)
	}
	s.addClient(c)
	defer func() {
		s.removeClient(c)
		c.Close()
		if s.OnDisconnect != nil {
			s.OnDisconnect(c)
		}
	}()
	dec := json.NewDecoder(c)
	for {
		var e Event
		err := dec.Decode(&e)
		if err != nil {
			if err != io.EOF {
				s.logErr(fmt.Errorf("decode event: %v", err))
			}
			break
		}
		if s.OnEvent != nil {
			s.OnEvent(c, e)
		}
		s.handle(c, e)
	}
}

func (s *Server) handle(c net.Conn, e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd, found := s.cmds[e.Name]
	var data interface{}
	if found {
		s.mu.Unlock()
		data = cmd(e.Data)
		s.mu.Lock()
	}
	re := Event{
		Name:  e.Name,
		Reply: true,
		Data:  data,
	}
	if _, err := c.Write(re.Bytes()); err != nil {
		s.logErr(err)
	}
}

func (s *Server) addClient(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = append(s.clients, c)
}

func (s *Server) removeClient(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clients := make([]net.Conn, 0, len(s.clients))
	for _, _c := range s.clients {
		if c == _c {
			continue
		}
		clients = append(clients, _c)
	}
	s.clients = clients
}

func (s *Server) logErr(err error) {
	if s.ErrorLog != nil {
		s.ErrorLog(err)
	}
}

// Stop stops listening on the named pipe.
func (s *Server) Stop() (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = nil
	if s.closer != nil {
		err = s.closer.Close()
		s.closer = nil
	}
	return
}
