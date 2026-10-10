package client

import (
	"net"
	"time"
)

// ReconnectLoop waits for connection loss and repeatedly attempts to reconnect
// with the last known reconnect token, re-sending un-acknowledged requests.
func (c *Client) ReconnectLoop(maxAttempts int, backoff time.Duration) {
	for {
		<-c.ConnectionLost()
		if c.closed() {
			return
		}
		if !c.tryReconnect(maxAttempts, backoff) {
			return
		}
		c.resendPending()
	}
}

func (c *Client) tryReconnect(attempts int, backoff time.Duration) bool {
	for i := 0; i < attempts; i++ {
		if c.closed() {
			return false
		}
		conn, err := net.DialTimeout("tcp", c.cfg.Address, 5*time.Second)
		if err == nil {
			c.refreshDone()
			if c.attach(conn) == nil {
				return true
			}
			c.close()
		}
		select {
		case <-time.After(backoff):
		case <-c.done:
			return false
		}
	}
	return false
}

// resendPending re-sends every un-acknowledged request with its original
// request_id and payload (server dedupes via idempotency cache).
func (c *Client) resendPending() {
	c.pending.Range(func(k, v interface{}) bool {
		pr := v.(*pendingReq)
		if err := c.send(pr.msg); err != nil {
			return false
		}
		return true
	})
}

func (c *Client) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
