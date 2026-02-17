package handler

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/procube-open/scep/depot/mysql"
	"github.com/procube-open/scep/utils"
)

type clientCertVerifyError struct {
	Message string
	Status  int
}

func verifyClientCertAndGetClient(depot *mysql.MySQLDepot, r *http.Request) (*x509.Certificate, *mysql.Client, *clientCertVerifyError) {
	encodedCert := r.Header["X-Mtls-Clientcert"]
	if len(encodedCert) != 1 {
		return nil, nil, &clientCertVerifyError{Message: "No Certificate", Status: http.StatusInternalServerError}
	}

	decodedCert, err := url.PathUnescape(encodedCert[0])
	if err != nil {
		return nil, nil, &clientCertVerifyError{Message: "Decode header failed", Status: http.StatusInternalServerError}
	}

	certBlock, _ := pem.Decode([]byte(decodedCert))
	if certBlock == nil {
		return nil, nil, &clientCertVerifyError{Message: "Parse Certificate failed", Status: http.StatusInternalServerError}
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, &clientCertVerifyError{Message: "Parse Certificate failed", Status: http.StatusInternalServerError}
	}

	now := time.Now()
	if now.After(cert.NotAfter) || now.Before(cert.NotBefore) {
		return nil, nil, &clientCertVerifyError{Message: "Certificate is expired", Status: http.StatusUnauthorized}
	}

	depotPath := utils.EnvString("SCEP_FILE_DEPOT", "ca-certs")
	caCrt, err := os.ReadFile(depotPath + "/ca.crt")
	if err != nil {
		return nil, nil, &clientCertVerifyError{Message: "Failed to read CA certificate", Status: http.StatusInternalServerError}
	}
	caCertBlock, _ := pem.Decode(caCrt)
	if caCertBlock == nil {
		return nil, nil, &clientCertVerifyError{Message: "Failed to parse CA certificate", Status: http.StatusInternalServerError}
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, &clientCertVerifyError{Message: "Failed to parse CA certificate", Status: http.StatusInternalServerError}
	}
	certPool := x509.NewCertPool()
	certPool.AddCert(caCert)
	opts := x509.VerifyOptions{
		Roots:     certPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	if _, err := cert.Verify(opts); err != nil {
		return nil, nil, &clientCertVerifyError{Message: "Failed to verify certificate", Status: http.StatusUnauthorized}
	}

	rcs, err := depot.GetRCs()
	if err != nil {
		return nil, nil, &clientCertVerifyError{Message: "Failed to get RCs", Status: http.StatusInternalServerError}
	}
	if checkIfRevoked(cert, rcs) {
		return nil, nil, &clientCertVerifyError{Message: "Certificate is revoked", Status: http.StatusUnauthorized}
	}

	client, err := depot.GetClient(cert.Subject.CommonName)
	if client == nil && err == nil {
		return nil, nil, &clientCertVerifyError{Message: "User Not Found", Status: http.StatusUnauthorized}
	}
	if err != nil {
		return nil, nil, &clientCertVerifyError{Message: err.Error(), Status: http.StatusInternalServerError}
	}

	return cert, client, nil
}
