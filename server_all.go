package ipc

import (
	"bufio"
	"errors"
	"time"
)

var (
	defaultServerConfig = ServerConfig{
		MaxMsgSize: defaultMaxMsgSize,
	}

	defaultClientConfig = ClientConfig{
		RetryTimer: defaultRetryTimer,
	}
)

// StartServer - starts the ipc server.
//
// ipcName = is the name of the unix socket or named pipe that will be created.
// timeout = number of seconds before the socket/pipe times out waiting for a connection/re-cconnection - if -1 or 0 it never times out.
//
func StartServer(ipcName string, config ServerConfig) (*Server, error) {
	err := checkIpcName(ipcName)
	if err != nil {
		return nil, err
	}

	sc := &Server{
		name:     ipcName,
		status:   NotConnected,
		received: make(chan *Message),
		toWrite:  make(chan *Message),
		conf:     config,
	}

	if sc.conf.Timeout <= 0 {
		sc.conf.Timeout = defaultServerConfig.Timeout
	}

	if sc.conf.MaxMsgSize < minMsgSize {
		sc.conf.MaxMsgSize = defaultServerConfig.MaxMsgSize
	}

	if sc.conf.SocketBasePath == "" {
		sc.conf.SocketBasePath = defaultServerConfig.SocketBasePath
	}

	go startServer(sc)

	return sc, err
}

func startServer(sc *Server) {

	err := sc.run()
	if err != nil {
		sc.received <- &Message{err: err, MsgType: -2}
	}
}

func (sc *Server) acceptLoop() {
	for {
		conn, err := sc.listen.Accept()
		if err != nil {
			break
		}

		if sc.status == Listening || sc.status == ReConnecting {

			sc.conn = conn

			err2 := sc.handshake()
			if err2 != nil {
				sc.received <- &Message{err: err2, MsgType: -2}
				sc.status = Error
				sc.listen.Close()
				sc.conn.Close()

			} else {
				go sc.read()
				go sc.write()

				sc.status = Connected
				sc.received <- &Message{Status: sc.status.String(), MsgType: -1}
				sc.connChannel <- true
			}

		}

	}

}

func (sc *Server) connectionTimer() error {
	if sc.conf.Timeout != 0 {
		ticker := time.NewTicker(sc.conf.Timeout)
		defer ticker.Stop()

		select {

		case <-sc.connChannel:
			return nil
		case <-ticker.C:
			sc.listen.Close()
			return errors.New("Timed out waiting for client to connect")
		}
	}

	select {

	case <-sc.connChannel:
		return nil
	}

}

func (sc *Server) read() {

	bLen := make([]byte, 4)

	for {

		res := sc.readData(bLen)
		if res == false {
			break
		}

		mLen := bytesToInt(bLen)

		msgRecvd := make([]byte, mLen)

		res = sc.readData(msgRecvd)
		if res == false {
			break
		}

		if sc.conf.Encryption {
			msgFinal, err := decrypt(*sc.enc.cipher, msgRecvd)
			if err != nil {
				sc.received <- &Message{err: err, MsgType: -2}
				continue
			}

			if bytesToInt(msgFinal[:4]) == 0 {
				//  type 0 = control message
			} else {
				sc.received <- &Message{Data: msgFinal[4:], MsgType: bytesToInt(msgFinal[:4])}
			}

		} else {
			if bytesToInt(msgRecvd[:4]) == 0 {
				//  type 0 = control message
			} else {
				sc.received <- &Message{Data: msgRecvd[4:], MsgType: bytesToInt(msgRecvd[:4])}
			}
		}

	}
}

func (sc *Server) readData(buff []byte) bool {

	_, err := sc.conn.Read(buff)
	if err != nil {
		if sc.status == Closing {
			sc.status = Closed
			return false
		}

		go sc.reConnect()
		return false

	}

	return true

}

func (sc *Server) reConnect() {

	sc.status = ReConnecting
	sc.received <- &Message{Status: sc.status.String(), MsgType: -1}

	err := sc.connectionTimer()
	if err != nil {
		sc.status = Timeout
		sc.received <- &Message{Status: sc.status.String(), MsgType: -1}

		sc.received <- &Message{err: err, MsgType: -2}

	}

}

// Read - blocking function that waits until an non multipart message is received

func (sc *Server) Read() (*Message, error) {

	m, ok := (<-sc.received)
	if ok == false {
		return nil, errors.New("the receive channel has been closed")
	}

	if m.err != nil {
		return nil, m.err
	}

	return m, nil

}

// Write - writes a non multipart message to the ipc connection.
// msgType - denotes the type of data being sent. 0 is a reserved type for internal messages and errors.
//
func (sc *Server) Write(msgType int, message []byte) error {

	if msgType == 0 {
		return errors.New("Message type 0 is reserved")
	}

	mlen := len(message)

	if mlen > sc.conf.MaxMsgSize {
		return errors.New("Message exceeds maximum message length")
	}

	if sc.status == Connected {

		sc.toWrite <- &Message{MsgType: msgType, Data: message}

	} else {
		return errors.New(sc.status.String())
	}

	return nil

}

func (sc *Server) write() {

	for {

		m, ok := <-sc.toWrite

		if ok == false {
			break
		}

		toSend := intToBytes(m.MsgType)

		writer := bufio.NewWriter(sc.conn)

		if sc.conf.Encryption {
			toSend = append(toSend, m.Data...)
			toSendEnc, err := encrypt(*sc.enc.cipher, toSend)
			if err != nil {
				//return err
			}
			toSend = toSendEnc
		} else {

			toSend = append(toSend, m.Data...)

		}

		writer.Write(intToBytes(len(toSend)))
		writer.Write(toSend)

		err := writer.Flush()
		if err != nil {
			//return err
		}

		time.Sleep(2 * time.Millisecond)

	}

}

// getStatus - get the current status of the connection
func (sc *Server) getStatus() Status {

	return sc.status

}

// StatusCode - returns the current connection status
func (sc *Server) StatusCode() Status {
	return sc.status
}

// Status - returns the current connection status as a string
func (sc *Server) Status() string {

	return sc.status.String()

}

// Close - closes the connection
func (sc *Server) Close() {
	sc.status = Closing

	if sc.listen != nil {
		sc.listen.Close()
	}

	if sc.conn != nil {
		sc.conn.Close()
	}

	if sc.received != nil {
		close(sc.received)
	}

	if sc.toWrite != nil {
		close(sc.toWrite)
	}
}
