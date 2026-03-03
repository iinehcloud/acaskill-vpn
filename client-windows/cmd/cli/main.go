package main

import (
"encoding/json"
"fmt"
"log"
"net"
"os"
"time"
)

type Message struct {
ID      string          `json:"id"`
Type    string          `json:"type"`
Payload json.RawMessage `json:"payload,omitempty"`
}

func call(msgType string, payload interface{}) (string, error) {
conn, err := net.DialTimeout("tcp", "127.0.0.1:47821", 5*time.Second)
if err != nil {
return "", fmt.Errorf("cannot connect to daemon (is it running?): %w", err)
}
defer conn.Close()
conn.SetDeadline(time.Now().Add(10 * time.Second))

var payloadBytes json.RawMessage
if payload != nil {
payloadBytes, _ = json.Marshal(payload)
}

req := Message{ID: "cli-1", Type: msgType, Payload: payloadBytes}
if err := json.NewEncoder(conn).Encode(req); err != nil {
return "", fmt.Errorf("send: %w", err)
}

var resp Message
if err := json.NewDecoder(conn).Decode(&resp); err != nil {
return "", fmt.Errorf("recv: %w", err)
}

return string(resp.Payload), nil
}

func main() {
if len(os.Args) < 2 {
fmt.Println("Usage: acaskill-cli <command>")
fmt.Println("Commands: status, interfaces, connect <name>, connect-all, disconnect-all")
os.Exit(1)
}

switch os.Args[1] {
case "status":
resp, err := call("GET_STATUS", nil)
if err != nil { log.Fatalf("Error: %v", err) }
var pretty interface{}
json.Unmarshal([]byte(resp), &pretty)
out, _ := json.MarshalIndent(pretty, "", "  ")
fmt.Println(string(out))

case "interfaces":
resp, err := call("GET_INTERFACES", nil)
if err != nil { log.Fatalf("Error: %v", err) }
var ifaces []map[string]interface{}
if err := json.Unmarshal([]byte(resp), &ifaces); err != nil {
fmt.Println(resp)
return
}
if len(ifaces) == 0 {
fmt.Println("No interfaces found")
return
}
for i, iface := range ifaces {
name := iface["friendlyName"]
ip := iface["ip"]
ifType := iface["type"]
active := iface["isActive"]
fmt.Printf("  %d. %-20s %-15v  type=%-10v active=%v\n", i+1, name, ip, ifType, active)
}

case "connect":
if len(os.Args) < 3 { log.Fatal("Usage: connect <interface-name>") }
resp, err := call("CONNECT_INTERFACE", map[string]string{"interfaceName": os.Args[2]})
if err != nil { log.Fatalf("Error: %v", err) }
fmt.Println(resp)

case "connect-all":
resp, err := call("CONNECT_ALL", nil)
if err != nil { log.Fatalf("Error: %v", err) }
fmt.Println(resp)

case "disconnect-all":
resp, err := call("DISCONNECT_ALL", nil)
if err != nil { log.Fatalf("Error: %v", err) }
fmt.Println(resp)

default:
log.Fatalf("Unknown command: %s", os.Args[1])
}
}
