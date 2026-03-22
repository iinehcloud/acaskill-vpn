package main

import (
	"fmt"
	"os"
	"unsafe"
	"golang.org/x/sys/unix"
)

const (
	TUNSETIFF   = 0x400454ca
	IFF_TUN     = 0x0001
	IFF_NO_PI   = 0x1000
)

type ifReq struct {
	Name  [16]byte
	Flags uint16
	_     [22]byte
}

func openTUN(name string) (*os.File, error) {
	fd, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}
	var req ifReq
	copy(req.Name[:], name)
	req.Flags = IFF_TUN | IFF_NO_PI
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd.Fd(), TUNSETIFF, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		fd.Close()
		return nil, fmt.Errorf("ioctl TUNSETIFF: %w", errno)
	}
	return fd, nil
}
