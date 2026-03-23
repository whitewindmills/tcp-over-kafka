package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

const (
	Version = 5

	MethodNoAuth          = 0x00
	MethodNoAcceptable    = 0xff
	CmdConnect            = 0x01
	AddrTypeIPv4          = 0x01
	AddrTypeDomain        = 0x03
	AddrTypeIPv6          = 0x04
	ReplySucceeded        = 0x00
	ReplyGeneralFailure   = 0x01
	ReplyCmdNotSupported  = 0x07
	ReplyAddrNotSupported = 0x08
)

var (
	ErrNoAcceptableMethods  = errors.New("socks5: no acceptable methods")
	ErrUnsupportedVersion   = errors.New("socks5: unsupported version")
	ErrCommandNotSupported  = errors.New("socks5: command not supported")
	ErrAddressNotSupported  = errors.New("socks5: address type not supported")
	ErrMalformedRequest     = errors.New("socks5: malformed request")
	ErrMalformedNegotiation = errors.New("socks5: malformed negotiation")
)

// RequestError carries a SOCKS5 reply code for protocol-level failures.
type RequestError struct {
	Reply byte
	Err   error
}

func (e *RequestError) Error() string {
	if e == nil || e.Err == nil {
		return "socks5: request error"
	}
	return e.Err.Error()
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Accept negotiates SOCKS5 and returns the requested target in host:port form.
func Accept(conn net.Conn) (string, error) {
	if err := Negotiate(conn); err != nil {
		return "", err
	}
	return ReadConnectRequest(conn)
}

// Connect speaks SOCKS5 as a client, requesting a CONNECT to the target host:port.
func Connect(conn net.Conn, target string) error {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("socks5: invalid port %q", portStr)
	}
	if _, err := conn.Write([]byte{Version, 0x01, MethodNoAuth}); err != nil {
		return err
	}

	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != Version || hdr[1] != MethodNoAuth {
		return ErrNoAcceptableMethods
	}

	atyp, addr, err := encodeAddress(host)
	if err != nil {
		return err
	}
	req := []byte{Version, CmdConnect, 0x00, atyp}
	req = append(req, addr...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}

	var resp [4]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return err
	}
	if resp[0] != Version {
		return ErrUnsupportedVersion
	}
	if resp[1] != ReplySucceeded {
		return &RequestError{Reply: resp[1], Err: fmt.Errorf("socks5: connect failed")}
	}
	switch resp[3] {
	case AddrTypeIPv4:
		if _, err := io.ReadFull(conn, make([]byte, 6)); err != nil {
			return err
		}
	case AddrTypeDomain:
		var n [1]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, make([]byte, int(n[0])+2)); err != nil {
			return err
		}
	case AddrTypeIPv6:
		if _, err := io.ReadFull(conn, make([]byte, 18)); err != nil {
			return err
		}
	default:
		return ErrAddressNotSupported
	}
	return nil
}

// Negotiate completes the SOCKS5 greeting/method selection exchange.
func Negotiate(conn net.Conn) error {
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != Version {
		return ErrUnsupportedVersion
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	if !contains(methods, MethodNoAuth) {
		_, _ = conn.Write([]byte{Version, MethodNoAcceptable})
		return ErrNoAcceptableMethods
	}
	_, err := conn.Write([]byte{Version, MethodNoAuth})
	return err
}

// ReadConnectRequest parses the SOCKS5 CONNECT request and returns the target address.
func ReadConnectRequest(conn net.Conn) (string, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return "", err
	}
	if hdr[0] != Version {
		return "", &RequestError{Reply: ReplyGeneralFailure, Err: ErrUnsupportedVersion}
	}
	if hdr[1] != CmdConnect {
		return "", &RequestError{Reply: ReplyCmdNotSupported, Err: ErrCommandNotSupported}
	}
	if hdr[2] != 0 {
		return "", &RequestError{Reply: ReplyGeneralFailure, Err: ErrMalformedRequest}
	}
	host, port, err := readAddress(conn, hdr[3])
	if err != nil {
		if re, ok := err.(*RequestError); ok {
			return "", re
		}
		return "", &RequestError{Reply: ReplyGeneralFailure, Err: err}
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// WriteReply emits a minimal SOCKS5 reply structure.
func WriteReply(conn net.Conn, rep byte) error {
	resp := []byte{
		Version,
		rep,
		0x00,
		AddrTypeIPv4,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
	}
	_, err := conn.Write(resp)
	return err
}

// readAddress parses the address portion of a SOCKS5 request.
func readAddress(r io.Reader, atyp byte) (string, uint16, error) {
	switch atyp {
	case AddrTypeIPv4:
		var addr [4]byte
		if _, err := io.ReadFull(r, addr[:]); err != nil {
			return "", 0, err
		}
		host := net.IP(addr[:]).String()
		port, err := readPort(r)
		if err != nil {
			return "", 0, err
		}
		return host, port, nil
	case AddrTypeDomain:
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return "", 0, err
		}
		host := make([]byte, n[0])
		if _, err := io.ReadFull(r, host); err != nil {
			return "", 0, err
		}
		port, err := readPort(r)
		if err != nil {
			return "", 0, err
		}
		return string(host), port, nil
	case AddrTypeIPv6:
		var addr [16]byte
		if _, err := io.ReadFull(r, addr[:]); err != nil {
			return "", 0, err
		}
		host := net.IP(addr[:]).String()
		port, err := readPort(r)
		if err != nil {
			return "", 0, err
		}
		return host, port, nil
	default:
		return "", 0, &RequestError{Reply: ReplyAddrNotSupported, Err: ErrAddressNotSupported}
	}
}

// readPort reads the port field from the SOCKS5 request.
func readPort(r io.Reader) (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

// encodeAddress serializes a host into the SOCKS5 address encoding.
func encodeAddress(host string) (byte, []byte, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return AddrTypeIPv4, append([]byte(nil), ip4...), nil
		}
		if ip16 := ip.To16(); ip16 != nil {
			return AddrTypeIPv6, append([]byte(nil), ip16...), nil
		}
	}
	hostBytes := []byte(host)
	if len(hostBytes) > 255 {
		return 0, nil, ErrAddressNotSupported
	}
	out := make([]byte, 1+len(hostBytes))
	out[0] = byte(len(hostBytes))
	copy(out[1:], hostBytes)
	return AddrTypeDomain, out, nil
}

func contains(values []byte, want byte) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func (e *RequestError) ReplyCode() byte {
	if e == nil {
		return ReplyGeneralFailure
	}
	return e.Reply
}

func (e *RequestError) String() string {
	return fmt.Sprintf("reply=%d err=%v", e.Reply, e.Err)
}
