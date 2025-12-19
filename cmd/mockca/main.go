// Package main provides a standalone Mock CA server for testing and development.
//
// The Mock CA server can be deployed as a Kubernetes pod or run as a standalone
// console application. It provides an HTTP API for certificate signing that
// mimics an external PKI system.
//
// Usage:
//
//	./mockca-server [flags]
//
// Flags:
//
//	-addr string      Address to listen on (default ":8080")
//	-log-level string Log level: debug, info, warn, error (default "info")
//	-log-format string Log format: json, text (default "text")
//	-ca-cn string     CA Common Name (default "Mock CA")
//	-ca-org string    CA Organization (default "cert-manager-external-issuer")
//	-ca-validity int  CA validity in years (default 10)
//	-cert-validity int Default certificate validity in days (default 365)
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	version = "1.0.0"
)

// Config holds the server configuration
type Config struct {
	Addr             string
	LogLevel         string
	LogFormat        string
	CACN             string
	CAOrg            string
	CAValidityYrs    int
	CertValidityDays int
}

// MockCA holds the CA state
type MockCA struct {
	caCert    *x509.Certificate
	caKey     *rsa.PrivateKey
	caPEM     []byte
	config    *Config
	logger    *slog.Logger
	signCount int64
	// certStore stores issued certificates keyed by subject CN for retrieval
	certStore map[string]*storedCert
}

// storedCert holds a certificate and its key for retrieval
type storedCert struct {
	CertPEM []byte
	KeyPEM  []byte
	CSR     []byte
	Subject string
}

// SignRequest represents a certificate signing request
type SignRequest struct {
	CSR          string `json:"csr"`
	ValidityDays int    `json:"validity_days,omitempty"`
	CommonName   string `json:"common_name,omitempty"`
}

// SignResponse represents a certificate signing response
type SignResponse struct {
	Certificate      string `json:"certificate"`
	CertificateChain string `json:"certificate_chain"`
	CA               string `json:"ca"`
	SerialNumber     string `json:"serial_number"`
	NotBefore        string `json:"not_before"`
	NotAfter         string `json:"not_after"`
	Subject          string `json:"subject"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	CA        string `json:"ca_subject"`
	CAExpires string `json:"ca_expires"`
	SignCount int64  `json:"certificates_signed"`
	Uptime    string `json:"uptime"`
}

var startTime = time.Now()

func main() {
	config := parseFlags()
	logger := setupLogger(config)

	logger.Info("Starting Mock CA Server",
		"version", version,
		"addr", config.Addr,
		"log_level", config.LogLevel,
	)

	// Initialize the Mock CA
	ca, err := NewMockCA(config, logger)
	if err != nil {
		logger.Error("Failed to initialize Mock CA", "error", err)
		os.Exit(1)
	}

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", ca.handleHealth)
	mux.HandleFunc("/healthz", ca.handleHealth)
	mux.HandleFunc("/readyz", ca.handleHealth)
	mux.HandleFunc("/sign", ca.handleSign)
	mux.HandleFunc("/api/v1/sign", ca.handleSign)
	mux.HandleFunc("/api/v1/certificate/sign", ca.handleSign)
	mux.HandleFunc("/cgi/pki.cgi", ca.handlePKISign) // Legacy PKI-compatible endpoint
	mux.HandleFunc("/ca", ca.handleGetCA)
	mux.HandleFunc("/", ca.handleRoot)

	// Create server with timeouts
	server := &http.Server{
		Addr:         config.Addr,
		Handler:      loggingMiddleware(logger, mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan bool)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		logger.Info("Shutting down server...")
		if err := server.Close(); err != nil {
			logger.Error("Server shutdown error", "error", err)
		}
		close(done)
	}()

	logger.Info("Mock CA Server is ready",
		"addr", config.Addr,
		"ca_subject", ca.caCert.Subject.String(),
		"ca_expires", ca.caCert.NotAfter.Format(time.RFC3339),
	)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}

	<-done
	logger.Info("Server stopped")
}

func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.Addr, "addr", ":8080", "Address to listen on")
	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level: debug, info, warn, error")
	flag.StringVar(&config.LogFormat, "log-format", "text", "Log format: json, text")
	flag.StringVar(&config.CACN, "ca-cn", "External Issuer Mock CA", "CA Common Name")
	flag.StringVar(&config.CAOrg, "ca-org", "cert-manager-external-issuer", "CA Organization")
	flag.IntVar(&config.CAValidityYrs, "ca-validity", 10, "CA validity in years")
	flag.IntVar(&config.CertValidityDays, "cert-validity", 365, "Default certificate validity in days")

	flag.Parse()

	// Override from environment variables
	if v := os.Getenv("MOCKCA_ADDR"); v != "" {
		config.Addr = v
	}
	if v := os.Getenv("MOCKCA_LOG_LEVEL"); v != "" {
		config.LogLevel = v
	}
	if v := os.Getenv("MOCKCA_LOG_FORMAT"); v != "" {
		config.LogFormat = v
	}

	return config
}

func setupLogger(config *Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(config.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	var handler slog.Handler
	if strings.ToLower(config.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// NewMockCA creates a new Mock CA with generated CA certificate
func NewMockCA(config *Config, logger *slog.Logger) (*MockCA, error) {
	logger.Debug("Generating CA private key", "bits", 2048)

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}
	logger.Debug("CA private key generated successfully")

	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
	}
	logger.Debug("CA serial number generated", "serial", serialNumber.String())

	caTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   config.CACN,
			Organization: []string{config.CAOrg},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(config.CAValidityYrs, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	logger.Debug("Creating CA certificate",
		"subject", caTemplate.Subject.String(),
		"not_before", caTemplate.NotBefore.Format(time.RFC3339),
		"not_after", caTemplate.NotAfter.Format(time.RFC3339),
	)

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertDER,
	})

	logger.Info("Mock CA initialized successfully",
		"ca_subject", caCert.Subject.String(),
		"ca_serial", caCert.SerialNumber.String(),
		"ca_not_before", caCert.NotBefore.Format(time.RFC3339),
		"ca_not_after", caCert.NotAfter.Format(time.RFC3339),
	)

	return &MockCA{
		caCert:    caCert,
		caKey:     caKey,
		caPEM:     caPEM,
		config:    config,
		logger:    logger,
		certStore: make(map[string]*storedCert),
	}, nil
}

func (ca *MockCA) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "Mock CA Server v%s\n\n", version)
	fmt.Fprintln(w, "Endpoints:")
	fmt.Fprintln(w, "  GET  /health              - Health check")
	fmt.Fprintln(w, "  GET  /ca                  - Get CA certificate (PEM)")
	fmt.Fprintln(w, "  POST /sign                - Sign a CSR (JSON)")
	fmt.Fprintln(w, "  POST /api/v1/sign         - Sign a CSR (JSON alternate)")
	fmt.Fprintln(w, "  POST /api/v1/certificate/sign - Sign a CSR (JSON alternate)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Legacy PKI-Compatible Endpoint:")
	fmt.Fprintln(w, "  POST /cgi/pki.cgi         - Legacy PKI API format")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  POST arguments (semicolon-separated):")
	fmt.Fprintln(w, "    getCERT     Return existing certificate")
	fmt.Fprintln(w, "    getKEY      Return existing certificate key")
	fmt.Fprintln(w, "    getCSR      Return existing CSR")
	fmt.Fprintln(w, "    new=1       Create new certificate or return existing")
	fmt.Fprintln(w, "    renew=1     Force recreation of certificate")
	fmt.Fprintln(w, "    subject     Full DN (e.g., /C=US/ST=California/L=San Francisco/O=Example/CN=example.com)")
	fmt.Fprintln(w, "    DNS2-DNS20  Subject Alternative Names")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  Example:")
	fmt.Fprintln(w, "    curl -s -X POST -d 'new=1;subject=/C=US/ST=California/L=San Francisco/O=Example/CN=test.com;DNS2=test2.com' http://mockca:8080/cgi/pki.cgi")
}

func (ca *MockCA) handleHealth(w http.ResponseWriter, r *http.Request) {
	ca.logger.Debug("Health check requested")

	response := HealthResponse{
		Status:    "healthy",
		Version:   version,
		CA:        ca.caCert.Subject.String(),
		CAExpires: ca.caCert.NotAfter.Format(time.RFC3339),
		SignCount: ca.signCount,
		Uptime:    time.Since(startTime).Round(time.Second).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ca *MockCA) handleGetCA(w http.ResponseWriter, r *http.Request) {
	ca.logger.Debug("CA certificate requested")

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=ca.crt")
	w.Write(ca.caPEM)
}

func (ca *MockCA) handleSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ca.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is supported", "")
		return
	}

	ca.logger.Debug("Certificate signing request received",
		"content_type", r.Header.Get("Content-Type"),
		"content_length", r.ContentLength,
	)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		ca.logger.Error("Failed to read request body", "error", err)
		ca.sendError(w, http.StatusBadRequest, "READ_ERROR", "Failed to read request body", err.Error())
		return
	}
	defer r.Body.Close()

	ca.logger.Debug("Request body received", "size", len(body))

	// Parse request - support both JSON and form-encoded
	var signReq SignRequest
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/json") {
		if err := json.Unmarshal(body, &signReq); err != nil {
			ca.logger.Error("Failed to parse JSON request", "error", err)
			ca.sendError(w, http.StatusBadRequest, "PARSE_ERROR", "Failed to parse JSON request", err.Error())
			return
		}
	} else {
		// Try to parse as form data or raw PEM
		if err := r.ParseForm(); err == nil && r.FormValue("csr") != "" {
			signReq.CSR = r.FormValue("csr")
		} else {
			// Assume body is raw PEM CSR
			signReq.CSR = string(body)
		}
	}

	if signReq.CSR == "" {
		ca.logger.Error("No CSR provided in request")
		ca.sendError(w, http.StatusBadRequest, "MISSING_CSR", "No CSR provided in request", "")
		return
	}

	ca.logger.Debug("CSR received", "csr_length", len(signReq.CSR))

	// Parse CSR
	csrPEM := signReq.CSR
	if !strings.HasPrefix(csrPEM, "-----BEGIN") {
		// Try base64 decoding
		ca.logger.Debug("CSR does not start with PEM header, assuming base64")
	}

	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		ca.logger.Error("Failed to decode CSR PEM")
		ca.sendError(w, http.StatusBadRequest, "INVALID_CSR", "Failed to decode CSR PEM", "CSR must be in PEM format")
		return
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		ca.logger.Error("Failed to parse CSR", "error", err)
		ca.sendError(w, http.StatusBadRequest, "INVALID_CSR", "Failed to parse CSR", err.Error())
		return
	}

	if err := csr.CheckSignature(); err != nil {
		ca.logger.Error("CSR signature validation failed", "error", err)
		ca.sendError(w, http.StatusBadRequest, "INVALID_SIGNATURE", "CSR signature validation failed", err.Error())
		return
	}

	ca.logger.Info("CSR parsed successfully",
		"subject", csr.Subject.String(),
		"dns_names", csr.DNSNames,
		"ip_addresses", csr.IPAddresses,
		"email_addresses", csr.EmailAddresses,
		"signature_algorithm", csr.SignatureAlgorithm.String(),
	)

	// Determine validity
	validityDays := ca.config.CertValidityDays
	if signReq.ValidityDays > 0 {
		validityDays = signReq.ValidityDays
	}

	// Generate serial number
	serialNumber, err := generateSerialNumber()
	if err != nil {
		ca.logger.Error("Failed to generate serial number", "error", err)
		ca.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate serial number", err.Error())
		return
	}

	// Create certificate
	notBefore := time.Now().Add(-1 * time.Minute)
	notAfter := time.Now().AddDate(0, 0, validityDays)

	certTemplate := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               csr.Subject,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              csr.DNSNames,
		IPAddresses:           csr.IPAddresses,
		URIs:                  csr.URIs,
		EmailAddresses:        csr.EmailAddresses,
	}

	ca.logger.Debug("Creating certificate",
		"serial", serialNumber.String(),
		"subject", csr.Subject.String(),
		"not_before", notBefore.Format(time.RFC3339),
		"not_after", notAfter.Format(time.RFC3339),
		"validity_days", validityDays,
	)

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, ca.caCert, csr.PublicKey, ca.caKey)
	if err != nil {
		ca.logger.Error("Failed to create certificate", "error", err)
		ca.sendError(w, http.StatusInternalServerError, "SIGNING_ERROR", "Failed to create certificate", err.Error())
		return
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Build certificate chain (cert + CA)
	certChain := string(certPEM) + string(ca.caPEM)

	ca.signCount++

	ca.logger.Info("Certificate signed successfully",
		"serial", serialNumber.String(),
		"subject", csr.Subject.String(),
		"dns_names", csr.DNSNames,
		"not_before", notBefore.Format(time.RFC3339),
		"not_after", notAfter.Format(time.RFC3339),
		"validity_days", validityDays,
		"total_signed", ca.signCount,
	)

	// Send response
	response := SignResponse{
		Certificate:      string(certPEM),
		CertificateChain: certChain,
		CA:               string(ca.caPEM),
		SerialNumber:     serialNumber.String(),
		NotBefore:        notBefore.Format(time.RFC3339),
		NotAfter:         notAfter.Format(time.RFC3339),
		Subject:          csr.Subject.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ca *MockCA) sendError(w http.ResponseWriter, status int, code, message, details string) {
	ca.logger.Warn("Sending error response",
		"status", status,
		"code", code,
		"message", message,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	})
}

func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialNumberLimit)
}

// handlePKISign handles the legacy PKI-compatible /cgi/pki.cgi endpoint
// This mimics legacy PKI API formats for testing
//
// POST arguments (semicolon-separated):
//   - getCERT     Return existing certificate
//   - getKEY      Return existing certificate key
//   - getCSR      Return existing CSR
//   - new=1       Create new certificate or return existing
//   - renew=1     Force recreation of certificate
//   - subject     Full DN (e.g., /C=US/ST=California/L=San Francisco/O=Example/CN=example.com)
//   - DNS2-DNS20  Subject Alternative Names
func (ca *MockCA) handlePKISign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
		return
	}

	ca.logger.Debug("PKI signing request received",
		"content_type", r.Header.Get("Content-Type"),
		"content_length", r.ContentLength,
	)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		ca.logger.Error("Failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ca.logger.Debug("PKI request body received", "body", string(body))

	// Parse semicolon-separated parameters
	params := parsePKIParams(string(body))

	ca.logger.Debug("Parsed PKI parameters", "params", params)

	// Get subject DN
	subjectDN := params["subject"]
	if subjectDN == "" {
		ca.logger.Error("No subject provided in request")
		http.Error(w, "subject parameter is required", http.StatusBadRequest)
		return
	}

	// Parse subject DN (format: /C=US/ST=California/L=San Francisco/O=Example/CN=example.com)
	subject := parseDN(subjectDN)
	cn := subject.CommonName
	if cn == "" {
		ca.logger.Error("No CN in subject DN", "subject", subjectDN)
		http.Error(w, "subject must contain CN", http.StatusBadRequest)
		return
	}

	// Collect DNS SANs
	dnsNames := []string{cn} // CN is always first SAN
	for i := 2; i <= 20; i++ {
		key := fmt.Sprintf("DNS%d", i)
		if dns, ok := params[key]; ok && dns != "" {
			dnsNames = append(dnsNames, dns)
		}
	}

	isNew := params["new"] == "1"
	isRenew := params["renew"] == "1"

	// Handle getCERT, getKEY, getCSR requests for existing certs
	if _, ok := params["getCERT"]; ok {
		stored, exists := ca.certStore[cn]
		if !exists {
			http.Error(w, "Certificate not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(stored.CertPEM)
		return
	}

	if _, ok := params["getKEY"]; ok {
		stored, exists := ca.certStore[cn]
		if !exists || stored.KeyPEM == nil {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(stored.KeyPEM)
		return
	}

	if _, ok := params["getCSR"]; ok {
		stored, exists := ca.certStore[cn]
		if !exists || stored.CSR == nil {
			http.Error(w, "CSR not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(stored.CSR)
		return
	}

	// Check for existing certificate if new=1 (not renew)
	if isNew && !isRenew {
		if stored, exists := ca.certStore[cn]; exists {
			ca.logger.Info("Returning existing certificate for CN", "cn", cn)
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Write(stored.CertPEM)
			w.Write(ca.caPEM) // Append CA cert
			return
		}
	}

	// Generate a new certificate
	ca.logger.Info("Generating new certificate",
		"cn", cn,
		"dns_names", dnsNames,
		"is_new", isNew,
		"is_renew", isRenew,
	)

	// Generate serial number
	serialNumber, err := generateSerialNumber()
	if err != nil {
		ca.logger.Error("Failed to generate serial number", "error", err)
		http.Error(w, "Failed to generate serial number", http.StatusInternalServerError)
		return
	}

	// Determine validity
	validityDays := ca.config.CertValidityDays
	notBefore := time.Now().Add(-1 * time.Minute)
	notAfter := time.Now().AddDate(0, 0, validityDays)

	// Generate key pair for the certificate
	certKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		ca.logger.Error("Failed to generate key pair", "error", err)
		http.Error(w, "Failed to generate key pair", http.StatusInternalServerError)
		return
	}

	// Create certificate template
	certTemplate := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              dnsNames,
	}

	// Sign the certificate with our CA
	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, ca.caCert, &certKey.PublicKey, ca.caKey)
	if err != nil {
		ca.logger.Error("Failed to create certificate", "error", err)
		http.Error(w, "Failed to create certificate", http.StatusInternalServerError)
		return
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certKey),
	})

	// Store the certificate for later retrieval
	ca.certStore[cn] = &storedCert{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
		Subject: subjectDN,
	}

	ca.signCount++

	ca.logger.Info("PKI certificate signed successfully",
		"serial", serialNumber.String(),
		"cn", cn,
		"dns_names", dnsNames,
		"not_before", notBefore.Format(time.RFC3339),
		"not_after", notAfter.Format(time.RFC3339),
		"total_signed", ca.signCount,
	)

	// Return certificate + CA chain as raw PEM (legacy format)
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Write(certPEM)
	w.Write(ca.caPEM)
}

// parsePKIParams parses semicolon-separated key=value parameters
// Example: "new=1;subject=/C=US/O=Example/CN=test.com;DNS2=alt.com"
func parsePKIParams(body string) map[string]string {
	params := make(map[string]string)

	// Split by semicolon
	parts := strings.Split(body, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check for key=value
		if idx := strings.Index(part, "="); idx > 0 {
			key := strings.TrimSpace(part[:idx])
			value := strings.TrimSpace(part[idx+1:])
			params[key] = value
		} else {
			// Key without value (e.g., "getCERT")
			params[part] = ""
		}
	}

	return params
}

// parseDN parses a DN string in the format /C=US/ST=California/L=San Francisco/O=Example/CN=example.com
func parseDN(dn string) pkix.Name {
	name := pkix.Name{}

	// Split by / and parse each component
	parts := strings.Split(dn, "/")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		idx := strings.Index(part, "=")
		if idx <= 0 {
			continue
		}

		key := strings.ToUpper(strings.TrimSpace(part[:idx]))
		value := strings.TrimSpace(part[idx+1:])

		switch key {
		case "CN":
			name.CommonName = value
		case "O":
			name.Organization = append(name.Organization, value)
		case "OU":
			name.OrganizationalUnit = append(name.OrganizationalUnit, value)
		case "L":
			name.Locality = append(name.Locality, value)
		case "ST":
			name.Province = append(name.Province, value)
		case "C":
			name.Country = append(name.Country, value)
		}
	}

	return name
}
