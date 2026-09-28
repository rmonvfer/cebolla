package fetch

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// SOCKSError is a non-success SOCKS5 reply. With "ExtendedErrors" on the
// SocksPort, tor reports why an onion connection failed (codes 0xF0-0xF7),
// which tells an offline service apart from an overloaded one.
type SOCKSError struct{ Code byte }

func (e *SOCKSError) Error() string { return fmt.Sprintf("socks reply 0x%02x (%s)", e.Code, e.Outcome()) }

// Outcome maps the reply to the fetch outcome recorded in fetch_log.
func (e *SOCKSError) Outcome() string {
	switch e.Code {
	case 0xF0:
		return "desc_not_found" // no descriptor published: service offline
	case 0xF1:
		return "desc_invalid"
	case 0xF2:
		return "intro_failed" // overloaded or flaky
	case 0xF3:
		return "rend_failed"
	case 0xF4, 0xF5:
		return "auth_required" // client authorization: never retried (read-only)
	case 0xF6:
		return "bad_address"
	case 0xF7:
		return "intro_timeout"
	case 0x05:
		return "refused" // service up, nothing on that port
	case 0x06:
		return "timeout"
	default:
		return "socks_error"
	}
}

// dialSOCKS opens a CONNECT through a SOCKS5 proxy, passing the hostname to
// the proxy (socks5h semantics: never resolved locally). user/pass become the
// isolation key with tor's IsolateSOCKSAuth, so each onion gets its own
// circuits.
func dialSOCKS(ctx context.Context, proxy, host string, port uint16, user string) (net.Conn, error) {
	if len(host) > 255 || len(user) > 255 {
		return nil, errors.New("socks: host or user too long")
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", proxy)
	if err != nil {
		return nil, fmt.Errorf("socks: dial proxy: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		c.SetDeadline(dl)
	}
	// Close the connection if ctx is cancelled mid-handshake.
	stop := context.AfterFunc(ctx, func() { c.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	if err := handshake(c, host, port, user); err != nil {
		c.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

func handshake(c net.Conn, host string, port uint16, user string) error {
	// Offer username/password only: tor accepts any credentials and uses them
	// as the isolation key.
	if _, err := c.Write([]byte{5, 1, 2}); err != nil {
		return err
	}
	var r [2]byte
	if _, err := io.ReadFull(c, r[:]); err != nil {
		return fmt.Errorf("socks: greeting: %w", err)
	}
	if r[0] != 5 || r[1] != 2 {
		return fmt.Errorf("socks: proxy refused user/pass auth (method 0x%02x)", r[1])
	}
	pass := "x"
	auth := append([]byte{1, byte(len(user))}, user...)
	auth = append(append(auth, byte(len(pass))), pass...)
	if _, err := c.Write(auth); err != nil {
		return err
	}
	if _, err := io.ReadFull(c, r[:]); err != nil {
		return fmt.Errorf("socks: auth: %w", err)
	}
	if r[1] != 0 {
		return errors.New("socks: auth rejected")
	}

	req := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err := c.Write(req); err != nil {
		return err
	}
	var h [4]byte
	if _, err := io.ReadFull(c, h[:]); err != nil {
		return fmt.Errorf("socks: reply: %w", err)
	}
	if h[1] != 0 {
		return &SOCKSError{Code: h[1]}
	}
	var skip int
	switch h[3] {
	case 1:
		skip = 4 + 2
	case 4:
		skip = 16 + 2
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(c, l[:]); err != nil {
			return err
		}
		skip = int(l[0]) + 2
	default:
		return fmt.Errorf("socks: bad address type %d", h[3])
	}
	_, err := io.CopyN(io.Discard, c, int64(skip))
	return err
}

// idleConn fails a read or write that makes no progress for d, so a service
// that trickles bytes cannot hold a worker forever.
type idleConn struct {
	net.Conn
	d time.Duration
}

func (c *idleConn) Read(b []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(c.d))
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(c.d))
	return c.Conn.Write(b)
}
