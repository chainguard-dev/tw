package control

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type Client struct {
	Addr string
}

func NewClient(addr string) *Client {
	return &Client{
		Addr: addr,
	}
}

func (c *Client) Send(cmd Command, testName string, exitCode int) error {
	msg := &Message{
		Command:  cmd,
		TestName: testName,
		ExitCode: exitCode,
		Time:     time.Now(),
	}

	conn, err := net.Dial("unix", c.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	decoder := json.NewDecoder(conn)
	var ack Ack
	if err := decoder.Decode(&ack); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	return nil
}
