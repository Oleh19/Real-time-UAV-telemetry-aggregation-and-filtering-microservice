package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	dir := flag.String("dir", "/certs", "output directory for generated certificates")
	serverNames := flag.String("server-names", "server,localhost", "comma-separated DNS names for the server certificate")
	validDays := flag.Int("valid-days", 365, "certificate validity in days")
	owner := flag.Int("owner-uid", 65532, "uid/gid to own the generated files so non-root services can read them (-1 to skip)")
	flag.Parse()

	if err := run(*dir, *serverNames, *validDays, *owner); err != nil {
		fmt.Fprintln(os.Stderr, "certgen:", err)
		os.Exit(1)
	}
}

func run(dir, serverNames string, validDays, owner int) error {
	if allPresent(dir) {
		fmt.Println("certgen: certificates already present, leaving them untouched")
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	defer func() { _ = chownAll(dir, owner) }()
	notAfter := time.Now().Add(time.Duration(validDays) * 24 * time.Hour)

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ca key: %w", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "uavmonitor-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create ca cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return fmt.Errorf("parse ca cert: %w", err)
	}
	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", caDER); err != nil {
		return err
	}

	dnsNames, ipAddresses := splitSANs(serverNames)
	if err := issueLeaf(dir, "server", caCert, caKey, notAfter, dnsNames, ipAddresses, true); err != nil {
		return err
	}
	if err := issueLeaf(dir, "client", caCert, caKey, notAfter, nil, nil, false); err != nil {
		return err
	}
	fmt.Println("certgen: wrote ca, server, and client certificates to", dir)
	return nil
}

func issueLeaf(dir, name string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, notAfter time.Time, dnsNames []string, ipAddresses []net.IP, isServer bool) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate %s key: %w", name, err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate %s serial: %w", name, err)
	}
	usage := x509.ExtKeyUsageClientAuth
	if isServer {
		usage = x509.ExtKeyUsageServerAuth
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "uavmonitor-" + name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create %s cert: %w", name, err)
	}
	if err := writePEM(filepath.Join(dir, name+".crt"), "CERTIFICATE", der); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal %s key: %w", name, err)
	}
	return writePEMFile(filepath.Join(dir, name+".key"), "PRIVATE KEY", keyDER, 0o600)
}

func splitSANs(serverNames string) ([]string, []net.IP) {
	var dnsNames []string
	var ipAddresses []net.IP
	for _, raw := range strings.Split(serverNames, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if ip := net.ParseIP(name); ip != nil {
			ipAddresses = append(ipAddresses, ip)
			continue
		}
		dnsNames = append(dnsNames, name)
	}
	return dnsNames, ipAddresses
}

func chownAll(dir string, owner int) error {
	if owner < 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		_ = os.Chown(filepath.Join(dir, entry.Name()), owner, owner)
	}
	return nil
}

func allPresent(dir string) bool {
	for _, name := range []string{"ca.crt", "server.crt", "server.key", "client.crt", "client.key"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}

func writePEM(path, blockType string, der []byte) error {
	return writePEMFile(path, blockType, der, 0o644)
}

func writePEMFile(path, blockType string, der []byte, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return nil
}
