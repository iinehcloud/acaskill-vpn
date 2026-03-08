package main

import (
"context"
"fmt"
"log"
"os"
"path/filepath"
"time"

"github.com/acaskill/vpn-client/internal/bonding"
"github.com/acaskill/vpn-client/internal/config"
"github.com/acaskill/vpn-client/internal/ipc"
	"github.com/acaskill/vpn-client/internal/proxy"
"golang.org/x/sys/windows/svc"
"golang.org/x/sys/windows/svc/debug"
"golang.org/x/sys/windows/svc/eventlog"
"golang.org/x/sys/windows/svc/mgr"
)

const (
ServiceName    = "AcaSkillVPN"
ServiceDisplay = "AcaSkill VPN Bonding Service"
ServiceDesc    = "Bonds multiple network interfaces via AcaSkill VPN"
)

func installService() error {
exepath, err := os.Executable()
if err != nil { return err }
m, err := mgr.Connect()
if err != nil { return err }
defer m.Disconnect()
s, err := m.OpenService(ServiceName)
if err == nil { s.Close(); return fmt.Errorf("service already exists") }
s, err = m.CreateService(ServiceName, exepath, mgr.Config{
DisplayName: ServiceDisplay,
Description: ServiceDesc,
StartType:   mgr.StartAutomatic,
}, "run")
if err != nil { return err }
defer s.Close()
eventlog.InstallAsEventCreate(ServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
fmt.Printf("Service %s installed\n", ServiceName)
return nil
}

func uninstallService() error {
m, err := mgr.Connect()
if err != nil { return err }
defer m.Disconnect()
s, err := m.OpenService(ServiceName)
if err != nil { return fmt.Errorf("service not found") }
defer s.Close()
s.Delete()
eventlog.Remove(ServiceName)
fmt.Printf("Service %s uninstalled\n", ServiceName)
return nil
}

type acaskillService struct {
cfg    *config.Config
bonder *bonding.Bonder
server *ipc.Server
}

func (s *acaskillService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
changes <- svc.Status{State: svc.StartPending}
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
s.bonder.Start(ctx)
bondProxy := proxy.New("127.0.0.1:1080")
if err := bondProxy.Start(ctx); err == nil {
s.bonder.SetProxy(bondProxy)
}
s.server.Start(ctx, s.bonder)
changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
for c := range r {
switch c.Cmd {
case svc.Interrogate:
changes <- c.CurrentStatus
case svc.Stop, svc.Shutdown:
changes <- svc.Status{State: svc.StopPending}
cancel()
time.Sleep(2 * time.Second)
changes <- svc.Status{State: svc.Stopped}
return false, 0
}
}
return false, 0
}

func runDebug() {
// Always log to stdout in debug mode
log.SetOutput(os.Stdout)
log.SetFlags(log.LstdFlags | log.Lshortfile)

log.Println("[daemon] starting in debug mode...")

cfg, err := config.Load()
if err != nil {
log.Fatalf("[daemon] config error: %v", err)
}

log.Printf("[daemon] config loaded. API: %s", cfg.APIBase)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

bonder := bonding.New(cfg)
ipcServer := ipc.NewServer(cfg)

if err := bonder.Start(ctx); err != nil {
log.Fatalf("[daemon] bonder start failed: %v", err)
}
log.Println("[daemon] bonding engine started")

// Start bonding proxy
bondProxy := proxy.New("127.0.0.1:1080")
if err := bondProxy.Start(ctx); err != nil {
log.Printf("[daemon] proxy start failed: %v", err)
} else {
bonder.SetProxy(bondProxy)
log.Println("[daemon] bonding proxy started on 127.0.0.1:1080")
}

if err := ipcServer.Start(ctx, bonder); err != nil {
log.Fatalf("[daemon] IPC server failed: %v", err)
}
log.Println("[daemon] IPC server listening on 127.0.0.1:47821")
log.Println("[daemon] ready. Press Ctrl+C to stop.")

// Block forever
<-ctx.Done()
}

func main() {
// In debug mode, always use stdout
if len(os.Args) > 1 && os.Args[1] == "debug" {
runDebug()
return
}

// Set up file logging for service mode
logDir := filepath.Join(os.Getenv("PROGRAMDATA"), "AcaSkillVPN", "logs")
os.MkdirAll(logDir, 0755)
logFile, err := os.OpenFile(filepath.Join(logDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
if err == nil {
log.SetOutput(logFile)
defer logFile.Close()
}
log.SetFlags(log.LstdFlags | log.Lshortfile)

if len(os.Args) > 1 {
switch os.Args[1] {
case "install":
if err := installService(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
return
case "uninstall":
if err := uninstallService(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
return
}
}

cfg, err := config.Load()
if err != nil { log.Fatalf("config: %v", err) }

isService, _ := svc.IsWindowsService()
bonder := bonding.New(cfg)
ipcServer := ipc.NewServer(cfg)
svcInst := &acaskillService{cfg: cfg, bonder: bonder, server: ipcServer}

if isService {
svc.Run(ServiceName, svcInst)
} else {
debug.Run(ServiceName, svcInst)
}
}
