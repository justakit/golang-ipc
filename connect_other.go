//go:build linux || darwin
// +build linux darwin

package ipc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Server create a unix socket and start listening connections - for unix and linux
func (sc *Server) run() error {
	socketPath := filepath.Join(sc.conf.SocketBasePath, sc.name+".sock")

	if err := os.RemoveAll(socketPath); err != nil {
		return err
	}

	var oldUmask int
	if sc.conf.UnmaskPermissions {
		oldUmask = syscall.Umask(0)
	}

	listen, err := net.Listen("unix", socketPath)

	if sc.conf.UnmaskPermissions {
		syscall.Umask(oldUmask)
	}

	if err != nil {
		return err
	}

	sc.listen = listen

	sc.status = Listening
	sc.received <- &Message{Status: sc.status.String(), MsgType: -1}
	sc.connChannel = make(chan bool)

	go sc.acceptLoop()

	err = sc.connectionTimer()
	if err != nil {
		return err
	}

	return nil

}

// Client connect to the unix socket created by the server -  for unix and linux
func (cc *Client) dial() error {
	socketPath := filepath.Join(cc.conf.SocketBasePath, cc.Name+".sock")

	startTime := time.Now()

	for {
		if cc.conf.Timeout != 0 {
			if time.Now().After(startTime.Add(cc.conf.Timeout)) {
				cc.status = Closed
				return errors.New("Timed out trying to connect")
			}
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			if strings.Contains(err.Error(), "connect: no such file or directory") == true {

			} else if strings.Contains(err.Error(), "connect: connection refused") == true {

			} else {
				cc.received <- &Message{err: err, MsgType: -2}
			}

		} else {

			cc.conn = conn

			err = cc.handshake()
			if err != nil {
				return err
			}

			return nil
		}

		time.Sleep(cc.conf.RetryTimer)

	}

}
