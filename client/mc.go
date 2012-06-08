// A memcached binary protocol client.
package memcached

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/dustin/gomemcached"
)

const bufsize = 1024

// The Client itself.
type Client struct {
	conn io.ReadWriteCloser

	hdrBuf []byte
}

// Connect to a memcached server.
func Connect(prot, dest string) (rv *Client, err error) {
	conn, err := net.Dial(prot, dest)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:   conn,
		hdrBuf: make([]byte, gomemcached.HDR_LEN),
	}, nil
}

// Close the connection when you're done.
func (c *Client) Close() {
	c.conn.Close()
}

// Send a custom request and get the response.
func (client *Client) Send(req *gomemcached.MCRequest) (rv *gomemcached.MCResponse, err error) {
	err = transmitRequest(client.conn, req)
	if err != nil {
		return
	}
	return getResponse(client.conn, client.hdrBuf)
}

// Send a request, but do not wait for a response.
func (client *Client) Transmit(req *gomemcached.MCRequest) error {
	return transmitRequest(client.conn, req)
}

// Receive a response
func (client *Client) Receive() (*gomemcached.MCResponse, error) {
	return getResponse(client.conn, client.hdrBuf)
}

// Get the value for a key.
func (client *Client) Get(vb uint16, key string) (*gomemcached.MCResponse, error) {
	return client.Send(&gomemcached.MCRequest{
		Opcode:  gomemcached.GET,
		VBucket: vb,
		Key:     []byte(key),
		Cas:     0,
		Opaque:  0,
		Extras:  []byte{},
		Body:    []byte{}})
}

// Delete a key.
func (client *Client) Del(vb uint16, key string) (*gomemcached.MCResponse, error) {
	return client.Send(&gomemcached.MCRequest{
		Opcode:  gomemcached.DELETE,
		VBucket: vb,
		Key:     []byte(key),
		Cas:     0,
		Opaque:  0,
		Extras:  []byte{},
		Body:    []byte{}})
}

func (client *Client) store(opcode gomemcached.CommandCode, vb uint16,
	key string, flags int, exp int, body []byte) (*gomemcached.MCResponse, error) {

	req := &gomemcached.MCRequest{
		Opcode:  opcode,
		VBucket: vb,
		Key:     []byte(key),
		Cas:     0,
		Opaque:  0,
		Extras:  []byte{0, 0, 0, 0, 0, 0, 0, 0},
		Body:    body}

	binary.BigEndian.PutUint64(req.Extras, uint64(flags)<<32|uint64(exp))
	return client.Send(req)
}

// Add a value for a key (store if not exists).
func (client *Client) Add(vb uint16, key string, flags int, exp int,
	body []byte) (*gomemcached.MCResponse, error) {
	return client.store(gomemcached.ADD, vb, key, flags, exp, body)
}

// Set the value for a key.
func (client *Client) Set(vb uint16, key string, flags int, exp int,
	body []byte) (*gomemcached.MCResponse, error) {
	return client.store(gomemcached.SET, vb, key, flags, exp, body)
}

// Stats returns a slice of these.
type StatValue struct {
	// The stat key
	Key string
	// The stat value
	Val string
}

// Get stats from the server
// use "" as the stat key for toplevel stats.
func (client *Client) Stats(key string) ([]StatValue, error) {
	rv := make([]StatValue, 0, 128)

	req := &gomemcached.MCRequest{
		Opcode:  gomemcached.STAT,
		VBucket: 0,
		Key:     []byte(key),
		Cas:     0,
		Opaque:  918494,
		Extras:  []byte{}}

	err := transmitRequest(client.conn, req)
	if err != nil {
		return rv, err
	}

	for {
		res, err := getResponse(client.conn, client.hdrBuf)
		if err != nil {
			return rv, err
		}
		k := string(res.Key)
		if k == "" {
			break
		}
		rv = append(rv, StatValue{
			Key: k,
			Val: string(res.Body),
		})
	}

	return rv, nil
}

// Get the stats from the server as a map
func (client *Client) StatsMap(key string) (map[string]string, error) {
	rv := make(map[string]string)
	st, err := client.Stats(key)
	if err != nil {
		return rv, err
	}
	for _, sv := range st {
		rv[sv.Key] = sv.Val
	}
	return rv, nil
}

func getResponse(s io.Reader, buf []byte) (rv *gomemcached.MCResponse, err error) {
	_, err = io.ReadFull(s, buf)
	if err != nil {
		return rv, err
	}
	rv, err = grokHeader(buf)
	if err != nil {
		return rv, err
	}
	err = readContents(s, rv)
	return rv, err
}

func readContents(s io.Reader, res *gomemcached.MCResponse) error {
	if len(res.Extras) > 0 {
		_, err := io.ReadFull(s, res.Extras)
		if err != nil {
			return err
		}
	}
	if len(res.Key) > 0 {
		_, err := io.ReadFull(s, res.Key)
		if err != nil {
			return err
		}
	}
	_, err := io.ReadFull(s, res.Body)
	return err
}

func grokHeader(hdrBytes []byte) (rv *gomemcached.MCResponse, err error) {
	if hdrBytes[0] != gomemcached.RES_MAGIC {
		return rv, fmt.Errorf("Bad magic: 0x%02x", hdrBytes[0])
	}
	rv = &gomemcached.MCResponse{
		Opcode: gomemcached.CommandCode(hdrBytes[1]),
		Key:    make([]byte, binary.BigEndian.Uint16(hdrBytes[2:4])),
		Extras: make([]byte, hdrBytes[4]),
		Status: gomemcached.Status(binary.BigEndian.Uint16(hdrBytes[6:8])),
		Opaque: binary.BigEndian.Uint32(hdrBytes[12:16]),
		Cas:    binary.BigEndian.Uint64(hdrBytes[16:24]),
	}
	bodyLen := binary.BigEndian.Uint32(hdrBytes[8:12]) -
		uint32(len(rv.Key)+len(rv.Extras))
	rv.Body = make([]byte, bodyLen)

	return
}

func transmitRequest(o io.Writer, req *gomemcached.MCRequest) (err error) {
	_, err = o.Write(req.Bytes())
	return
}
