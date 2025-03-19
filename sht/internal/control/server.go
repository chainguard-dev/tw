package control

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
)

type Handler func(Message) error

type Server struct {
	listener net.Listener
	handler  Handler
}

func NewServer(handler Handler) *Server {
	return &Server{
		handler:  handler,
		listener: nil,
	}
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) Start() error {
	if s.handler == nil {
		return fmt.Errorf("no handler registered")
	}

	tdir, err := os.MkdirTemp("", "sht-ctrl")
	if err != nil {
		return err
	}

	sock := filepath.Join(tdir, "sht-ctrl.sock")
	s.listener, err = net.Listen("unix", sock)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if _, ok := err.(net.Error); ok {
					continue
				}
				return
			}

			go s.handle(conn)
		}
	}()

	return nil
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	var msg Message
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("Error decoding message: %v", err)
		conn.Write([]byte("{}"))
		return
	}

	if err := s.handler(msg); err != nil {
		log.Printf("Error handling message: %v", err)
	}

	// ack it
	if _, err := conn.Write([]byte("{}")); err != nil {
		log.Printf("Error acking: %v", err)
	}
}

func (s *Server) Stop() error {
	if s.listener == nil {
		return nil
	}

	if err := s.listener.Close(); err != nil {
		return err
	}

	return os.RemoveAll(s.listener.Addr().String())
}
