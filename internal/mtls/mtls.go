package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func DialCredentials(f Files, serverName string) (grpc.DialOption, bool, error) {
	if !f.ClientEnabled() {
		return grpc.WithTransportCredentials(insecure.NewCredentials()), false, nil
	}
	creds, err := ClientCredentials(f, serverName)
	if err != nil {
		return nil, false, err
	}
	return grpc.WithTransportCredentials(creds), true, nil
}

type Files struct {
	CACert     string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

func FilesFromEnv() Files {
	return Files{
		CACert:     os.Getenv("TLS_CA_CERT"),
		ServerCert: os.Getenv("TLS_SERVER_CERT"),
		ServerKey:  os.Getenv("TLS_SERVER_KEY"),
		ClientCert: os.Getenv("TLS_CLIENT_CERT"),
		ClientKey:  os.Getenv("TLS_CLIENT_KEY"),
	}
}

func (f Files) ServerEnabled() bool {
	return f.CACert != "" && f.ServerCert != "" && f.ServerKey != ""
}

func (f Files) ClientEnabled() bool {
	return f.CACert != "" && f.ClientCert != "" && f.ClientKey != ""
}

func ServerCredentials(f Files) (credentials.TransportCredentials, error) {
	pool, err := certPool(f.CACert)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(f.ServerCert, f.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

func ClientCredentials(f Files, serverName string) (credentials.TransportCredentials, error) {
	pool, err := certPool(f.CACert)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(f.ClientCert, f.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("load client keypair: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

func certPool(caCert string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(caCert)
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("ca cert file contained no valid certificates")
	}
	return pool, nil
}
