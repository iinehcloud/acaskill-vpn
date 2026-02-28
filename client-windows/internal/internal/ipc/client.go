package ipc

import (
"encoding/json"
"fmt"
"net"
"sync/atomic"
"time"
)

var msgCounter uint64
func nextID() string { return fmt.Sprintf("msg-%d", atomic.AddUint64(&msgCounter, 1)) }

type Client struct {
conn    net.Conn
encoder *json.Encoder
decoder *json.Decoder
}

func Connect() (*Client, error) {
conn, err := net.DialTimeout("tcp", "127.0.0.1:47821", 3*time.Second)
if err != nil { return nil, fmt.Errorf("connect to daemon: %w", err) }
return &Client{conn: conn, encoder: json.NewEncoder(conn), decoder: json.NewDecoder(conn)}, nil
}

func (c *Client) Close() { c.conn.Close() }

func (c *Client) send(msgType MessageType, payload interface{}) (Response, error) {
var raw json.RawMessage
if payload != nil { b, _ := json.Marshal(payload); raw = b }
req := Request{ID: nextID(), Type: msgType, Payload: raw}
c.conn.SetDeadline(time.Now().Add(10 * time.Second))
if err := c.encoder.Encode(req); err != nil { return Response{}, err }
var resp Response
if err := c.decoder.Decode(&resp); err != nil { return Response{}, err }
if resp.Type == MsgError {
var e ErrorPayload
json.Unmarshal(resp.Payload, &e)
return resp, fmt.Errorf("daemon error: %s", e.Message)
}
return resp, nil
}

func (c *Client) GetStatus() (map[string]interface{}, error) {
resp, err := c.send(MsgGetStatus, nil)
if err != nil { return nil, err }
var s map[string]interface{}
json.Unmarshal(resp.Payload, &s)
return s, nil
}

func (c *Client) GetInterfaces() ([]map[string]interface{}, error) {
resp, err := c.send(MsgGetInterfaces, nil)
if err != nil { return nil, err }
var ifaces []map[string]interface{}
json.Unmarshal(resp.Payload, &ifaces)
return ifaces, nil
}

func (c *Client) ConnectInterface(name string) error {
_, err := c.send(MsgConnectInterface, ConnectInterfacePayload{InterfaceName: name}); return err
}

func (c *Client) DisconnectInterface(name string) error {
_, err := c.send(MsgDisconnectInterface, ConnectInterfacePayload{InterfaceName: name}); return err
}

func (c *Client) ConnectAll() error { _, err := c.send(MsgConnectAll, nil); return err }
func (c *Client) DisconnectAll() error { _, err := c.send(MsgDisconnectAll, nil); return err }
