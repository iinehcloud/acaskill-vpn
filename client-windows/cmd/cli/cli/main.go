package main

import (
"encoding/json"
"fmt"
"os"
"github.com/acaskill/vpn-client/internal/ipc"
)

func main() {
if len(os.Args) < 2 { usage(); os.Exit(1) }
c, err := ipc.Connect()
if err != nil { fmt.Fprintln(os.Stderr, "Error:", err); os.Exit(1) }
defer c.Close()
switch os.Args[1] {
case "status":
s, err := c.GetStatus()
if err != nil { fatal(err) }
data, _ := json.MarshalIndent(s, "", "  ")
fmt.Println(string(data))
case "interfaces":
ifaces, err := c.GetInterfaces()
if err != nil { fatal(err) }
for i, iface := range ifaces {
fmt.Printf("  %d. %v [%v]\n", i+1, iface["friendlyName"], iface["type"])
}
case "connect":
if len(os.Args) < 3 { fmt.Fprintln(os.Stderr, "Usage: acaskill-cli connect <name>"); os.Exit(1) }
if err := c.ConnectInterface(os.Args[2]); err != nil { fatal(err) }
fmt.Println("Connected:", os.Args[2])
case "connect-all":
if err := c.ConnectAll(); err != nil { fatal(err) }
fmt.Println("Connecting all interfaces...")
case "disconnect-all":
if err := c.DisconnectAll(); err != nil { fatal(err) }
fmt.Println("Disconnected.")
default:
usage(); os.Exit(1)
}
}

func usage() { fmt.Println("Commands: status, interfaces, connect <name>, connect-all, disconnect-all") }
func fatal(err error) { fmt.Fprintln(os.Stderr, "Error:", err); os.Exit(1) }
