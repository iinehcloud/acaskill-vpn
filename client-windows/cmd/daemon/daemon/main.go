// =============================================================================
// AcaSkill VPN - Windows Daemon
// Runs as a Windows Service. Bonds multiple network interfaces into one fast
// connection via the EU aggregation server.
//
// Build:
//   GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui -s -w" -o acaskill-daemon.exe ./cmd/daemon
// =============================================================================

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
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	ServiceName    = "AcaSkillVPN"
	ServiceDisplay = "AcaSkill VPN Bonding Service"
	ServiceDesc    = "Bonds multiple network interfaces for higher speed via AcaSkill VPN"
)

// ── Service install/uninstall helpers (called from CLI) ───────────────────────

func installService() error {
	exepath, err := os.Executable()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", ServiceName)
	}

	s, err = m.CreateService(ServiceName, exepath,
		mgr.Config{
			DisplayName: ServiceDisplay,
			Description: ServiceDesc,
			StartType:   mgr.StartAutomatic,
		},
		"run",
	)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	err = eventlog.InstallAsEventCreate(ServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("install event log: %w", err)
	}

	fmt.Printf("Service %s installed successfully\n", ServiceName)
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s not found", ServiceName)
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		return err
	}

	err = eventlog.Remove(ServiceName)
	if err != nil {
		return fmt.Errorf("remove event log: %w", err)
	}

	fmt.Printf("Service %s uninstalled\n", ServiceName)
	return nil
}

// ── Windows Service implementation ───────────────────────────────────────────

type acaskillService struct {
	cfg    *config.Config
	bonder *bonding.Bonder
	server *ipc.Server
}

func (s *acaskillService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the bonding engine
	if err := s.bonder.Start(ctx); err != nil {
		log.Printf("Failed to start bonder: %v", err)
		changes <- svc.Status{State: svc.Stopped}
		return
	}

	// Start IPC server (GUI communicates through this)
	if err := s.server.Start(ctx, s.bonder); err != nil {
		log.Printf("Failed to start IPC server: %v", err)
	}

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				time.Sleep(2 * time.Second)
				changes <- svc.Status{State: svc.Stopped}
				return
			}
		}
	}
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	// Set up logging to file
	logDir := filepath.Join(os.Getenv("PROGRAMDATA"), "AcaSkillVPN", "logs")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(
		filepath.Join(logDir, "daemon.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
	)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Handle CLI commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := installService(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if err := uninstallService(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "debug":
			// Run in console (not as service) for development
			runDebug()
			return
		}
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Detect if running as service or interactively
	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("Failed to detect service mode: %v", err)
	}

	bonder := bonding.New(cfg)
	ipcServer := ipc.NewServer()
	svcInstance := &acaskillService{cfg: cfg, bonder: bonder, server: ipcServer}

	if isService {
		if err := svc.Run(ServiceName, svcInstance); err != nil {
			log.Fatalf("Service failed: %v", err)
		}
	} else {
		// Running interactively (e.g. started by GUI directly)
		if err := debug.Run(ServiceName, svcInstance); err != nil {
			log.Fatalf("Debug run failed: %v", err)
		}
	}
}

func runDebug() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bonder := bonding.New(cfg)
	ipcServer := ipc.NewServer()

	if err := bonder.Start(ctx); err != nil {
		log.Fatalf("Bonder start failed: %v", err)
	}

	if err := ipcServer.Start(ctx, bonder); err != nil {
		log.Fatalf("IPC start failed: %v", err)
	}

	log.Println("AcaSkill VPN daemon running in debug mode. Press Ctrl+C to stop.")
	select {}
}
