package vv

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// ListenAndServeTLS -----------------------------------------------------------------------------
func ListenAndServeTLS(srv *http.Server, certPEMBlock []byte, keyPEMBlock []byte) error {
	addr := srv.Addr
	if addr == "" {
		addr = ":https"
	}

	config := &tls.Config{}
	if srv.TLSConfig != nil {
		*config = *srv.TLSConfig
	}
	if config.NextProtos == nil {
		config.NextProtos = []string{"http/1.1"}
	}

	var err error
	config.Certificates = make([]tls.Certificate, 1)
	config.Certificates[0], err = tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	//config.Certificates[0], err = tls.LoadX509KeyPair("cert.pem", "key.pem") // ключи тупо из файлов

	if err != nil {
		Vlogger.Vlog(0, err.Error(), 1)
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		Vlogger.Vlog(0, err.Error(), 1)
		return err
	}

	tlsListener := tls.NewListener(tcpKeepAliveListener{ln.(*net.TCPListener)}, config)
	return srv.Serve(tlsListener)
}

//-----------------------------------------------------------------------------
// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually go away.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

// Accept -----------------------------------------------------------------------------
func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

//-----------------------------------------------------------------------------
