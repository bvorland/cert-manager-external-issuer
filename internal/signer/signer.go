package signer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PKIConfig holds configuration for connecting to an external PKI API
type PKIConfig struct {
	// BaseURL is the full URL to the PKI API endpoint
	BaseURL string `json:"baseUrl"`

	// Method is the HTTP method to use (GET or POST)
	Method string `json:"method"`

	// Parameters configures how to build the request
	Parameters PKIParameters `json:"parameters"`

	// Response configures how to parse the response
	Response PKIResponse `json:"response"`

	// Auth configures authentication
	Auth *PKIAuth `json:"auth,omitempty"`

	// TLS configures TLS settings
	TLS *PKITLS `json:"tls,omitempty"`
}

// PKIParameters configures request parameters for the PKI API
type PKIParameters struct {
	// ParamFormat is the parameter format: "ampersand" (default) or "semicolon" (legacy PKI format)
	ParamFormat string `json:"paramFormat"`

	// NewCertParam is the parameter name for new certificate requests
	NewCertParam string `json:"newCertParam"`

	// NewCertValue is the value to send for new certificate requests
	NewCertValue string `json:"newCertValue"`

	// RenewCertParam is the parameter name for renewal requests
	RenewCertParam string `json:"renewCertParam"`

	// RenewCertValue is the value to send for renewal requests
	RenewCertValue string `json:"renewCertValue"`

	// SubjectParam is the parameter name for the certificate subject DN
	SubjectParam string `json:"subjectParam"`

	// SubjectDNFormat is the DN format: "comma" (default) or "slash" (legacy format: /C=US/ST=California/L=San Francisco/O=Example/CN=...)
	SubjectDNFormat string `json:"subjectDNFormat"`

	// DNSPrefix is the prefix for SAN DNS parameters (e.g., "DNS" -> "DNS2", "DNS3")
	DNSPrefix string `json:"dnsPrefix"`

	// DNSStartIndex is the starting index for DNS parameters (default: 2)
	DNSStartIndex int `json:"dnsStartIndex"`

	// DNSMaxCount is the maximum number of DNS SANs to include
	DNSMaxCount int `json:"dnsMaxCount"`

	// GetCertParam is the parameter to request certificate in response
	GetCertParam string `json:"getCertParam"`

	// GetKeyParam is the parameter to request private key (rarely used)
	GetKeyParam string `json:"getKeyParam"`

	// GetCSRParam is the parameter name to send the CSR
	GetCSRParam string `json:"getCSRParam"`
}

// PKIResponse configures how to parse the PKI API response
type PKIResponse struct {
	// Format is the response format: "pem", "json", "base64"
	Format string `json:"format"`

	// CertificateField is the JSON field containing the certificate (if format=json)
	CertificateField string `json:"certificateField,omitempty"`

	// ChainField is the JSON field containing the CA chain (if format=json)
	ChainField string `json:"chainField,omitempty"`
}

// PKIAuth configures authentication for the PKI API
type PKIAuth struct {
	// Type is the authentication type: "bearer", "basic", "header", "none"
	Type string `json:"type"`

	// HeaderName is the custom header name (for type=header)
	HeaderName string `json:"headerName,omitempty"`

	// SecretRef is the name of the Secret containing credentials
	SecretRef string `json:"secretRef,omitempty"`
}

// PKITLS configures TLS settings for the PKI API connection
type PKITLS struct {
	// InsecureSkipVerify skips TLS certificate verification (NOT recommended for production)
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CASecretRef is the name of a Secret containing the CA certificate to trust
	CASecretRef string `json:"caSecretRef,omitempty"`
}

// PKISigner implements certificate signing via an external PKI API
type PKISigner struct {
	config     *PKIConfig
	httpClient *http.Client
	authToken  string
}

// NewPKISigner creates a new PKI signer with the given configuration
func NewPKISigner(config *PKIConfig) *PKISigner {
	client := &http.Client{Timeout: 60 * time.Second}

	// Configure TLS settings if specified
	if config.TLS != nil && config.TLS.InsecureSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // Explicitly configured by user for testing
			},
		}
	}

	return &PKISigner{
		config:     config,
		httpClient: client,
	}
}

// SetAuthToken sets the authentication token for API requests
func (s *PKISigner) SetAuthToken(token string) {
	s.authToken = token
}

// CheckHealth verifies connectivity to the PKI API
func (s *PKISigner) CheckHealth() error {
	req, err := http.NewRequest("GET", s.config.BaseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}
	s.addAuth(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to PKI API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PKI API error: %d, %s", resp.StatusCode, string(body))
	}

	return nil
}

// Sign signs a CSR using the external PKI API
func (s *PKISigner) Sign(csrPEM []byte, validityDays int) ([]byte, []byte, error) {
	// Parse the CSR
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, nil, fmt.Errorf("invalid CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CSR: %w", err)
	}

	// Build request parameters
	params := s.buildRequestParams(csr)

	// Make the signing request
	certPEM, err := s.makeRequest(params)
	if err != nil {
		return nil, nil, err
	}

	// Extract CA chain from the full certificate chain
	caPEM := s.extractCAChain(certPEM)

	return certPEM, caPEM, nil
}

// buildRequestParams builds HTTP request parameters from the CSR
func (s *PKISigner) buildRequestParams(csr *x509.CertificateRequest) url.Values {
	params := url.Values{}
	cfg := s.config.Parameters

	// Add new certificate action parameter
	if cfg.NewCertParam != "" {
		params.Set(cfg.NewCertParam, cfg.NewCertValue)
	}

	// Build subject DN
	subject := s.buildSubjectDN(csr)
	if cfg.SubjectParam != "" && subject != "" {
		params.Set(cfg.SubjectParam, subject)
	}

	// Add DNS SANs
	if len(csr.DNSNames) > 0 && cfg.DNSPrefix != "" {
		startIdx := cfg.DNSStartIndex
		if startIdx == 0 {
			startIdx = 2 // Default start index
		}
		maxCount := cfg.DNSMaxCount
		if maxCount == 0 {
			maxCount = 20 // Default max
		}

		for i, dns := range csr.DNSNames {
			if i >= maxCount {
				break
			}
			params.Set(fmt.Sprintf("%s%d", cfg.DNSPrefix, startIdx+i), dns)
		}
	}

	// Add certificate format request
	if cfg.GetCertParam != "" {
		params.Set(cfg.GetCertParam, "")
	}

	return params
}

// buildSubjectDN builds a subject DN string from the CSR
func (s *PKISigner) buildSubjectDN(csr *x509.CertificateRequest) string {
	// Check if using slash format (legacy PKI format: /C=US/ST=California/L=San Francisco/O=Example/CN=example.com)
	if s.config.Parameters.SubjectDNFormat == "slash" {
		return s.buildSubjectDNSlash(csr)
	}
	// Default comma-separated format: CN=...,O=...,C=...
	return s.buildSubjectDNComma(csr)
}

// buildSubjectDNSlash builds a DN in slash format: /C=US/ST=California/L=San Francisco/O=Example/CN=example.com
func (s *PKISigner) buildSubjectDNSlash(csr *x509.CertificateRequest) string {
	var parts []string

	// Note: Slash format uses reverse order (most general first)
	for _, c := range csr.Subject.Country {
		parts = append(parts, "/C="+c)
	}
	for _, st := range csr.Subject.Province {
		parts = append(parts, "/ST="+st)
	}
	for _, l := range csr.Subject.Locality {
		parts = append(parts, "/L="+l)
	}
	for _, o := range csr.Subject.Organization {
		parts = append(parts, "/O="+o)
	}
	for _, ou := range csr.Subject.OrganizationalUnit {
		parts = append(parts, "/OU="+ou)
	}
	if csr.Subject.CommonName != "" {
		parts = append(parts, "/CN="+csr.Subject.CommonName)
	}

	// Fallback to first DNS name if no CN
	if len(parts) == 0 && len(csr.DNSNames) > 0 {
		parts = append(parts, "/CN="+csr.DNSNames[0])
	}

	return strings.Join(parts, "")
}

// buildSubjectDNComma builds a DN in comma format: CN=...,O=...,C=...
func (s *PKISigner) buildSubjectDNComma(csr *x509.CertificateRequest) string {
	var parts []string

	if csr.Subject.CommonName != "" {
		parts = append(parts, "CN="+csr.Subject.CommonName)
	}
	for _, ou := range csr.Subject.OrganizationalUnit {
		parts = append(parts, "OU="+ou)
	}
	for _, o := range csr.Subject.Organization {
		parts = append(parts, "O="+o)
	}
	for _, l := range csr.Subject.Locality {
		parts = append(parts, "L="+l)
	}
	for _, st := range csr.Subject.Province {
		parts = append(parts, "ST="+st)
	}
	for _, c := range csr.Subject.Country {
		parts = append(parts, "C="+c)
	}

	// Fallback to first DNS name if no CN
	if len(parts) == 0 && len(csr.DNSNames) > 0 {
		parts = append(parts, "CN="+csr.DNSNames[0])
	}

	return strings.Join(parts, ",")
}

// makeRequest sends the signing request to the PKI API
func (s *PKISigner) makeRequest(params url.Values) ([]byte, error) {
	method := strings.ToUpper(s.config.Method)
	if method == "" {
		method = "POST"
	}

	// Build request body based on format
	var body string
	if s.config.Parameters.ParamFormat == "semicolon" {
		// Legacy PKI format: key=value;key2=value2
		var parts []string
		for key, values := range params {
			if len(values) > 0 && values[0] != "" {
				parts = append(parts, key+"="+values[0])
			} else if len(values) > 0 {
				parts = append(parts, key)
			}
		}
		body = strings.Join(parts, ";")
	} else {
		// Standard URL-encoded format: key=value&key2=value2
		body = params.Encode()
	}

	var req *http.Request
	var err error

	if method == "GET" {
		if s.config.Parameters.ParamFormat == "semicolon" {
			req, err = http.NewRequest("GET", s.config.BaseURL+"?"+body, nil)
		} else {
			req, err = http.NewRequest("GET", s.config.BaseURL+"?"+params.Encode(), nil)
		}
	} else {
		req, err = http.NewRequest("POST", s.config.BaseURL, strings.NewReader(body))
		if s.config.Parameters.ParamFormat == "semicolon" {
			req.Header.Set("Content-Type", "text/plain")
		} else {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	s.addAuth(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PKI API error: %d, %s", resp.StatusCode, string(respBody))
	}

	return s.parseResponse(respBody)
}

// parseResponse parses the PKI API response based on configured format
func (s *PKISigner) parseResponse(body []byte) ([]byte, error) {
	format := s.config.Response.Format
	if format == "" {
		format = "pem"
	}

	// For PEM format, check if response contains a certificate
	if !strings.Contains(string(body), "-----BEGIN CERTIFICATE-----") {
		return nil, fmt.Errorf("no certificate in response")
	}

	return body, nil
}

// extractCAChain extracts the CA chain from a full certificate chain
// The first certificate is the leaf, remaining are the CA chain
func (s *PKISigner) extractCAChain(fullChain []byte) []byte {
	var caCerts []string
	remaining := string(fullChain)
	isFirst := true

	for {
		start := strings.Index(remaining, "-----BEGIN CERTIFICATE-----")
		if start == -1 {
			break
		}
		end := strings.Index(remaining[start:], "-----END CERTIFICATE-----")
		if end == -1 {
			break
		}

		cert := remaining[start : start+end+len("-----END CERTIFICATE-----")]
		if !isFirst {
			caCerts = append(caCerts, strings.TrimSpace(cert))
		}
		isFirst = false
		remaining = remaining[start+end+len("-----END CERTIFICATE-----"):]
	}

	if len(caCerts) == 0 {
		return nil
	}

	return []byte(strings.Join(caCerts, "\n"))
}

// addAuth adds authentication headers to the request
func (s *PKISigner) addAuth(req *http.Request) {
	if s.config.Auth == nil {
		return
	}

	switch s.config.Auth.Type {
	case "header":
		if s.authToken != "" {
			req.Header.Set(s.config.Auth.HeaderName, s.authToken)
		}
	case "basic":
		if s.authToken != "" {
			req.Header.Set("Authorization", "Basic "+s.authToken)
		}
	case "bearer":
		if s.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+s.authToken)
		}
	}
}

// DefaultPKIConfig returns a default PKI configuration template
func DefaultPKIConfig() *PKIConfig {
	return &PKIConfig{
		BaseURL: "https://pki.example.com/api/sign",
		Method:  "POST",
		Parameters: PKIParameters{
			NewCertParam:   "action",
			NewCertValue:   "new",
			RenewCertParam: "action",
			RenewCertValue: "renew",
			SubjectParam:   "subject",
			DNSPrefix:      "dns_san",
			DNSStartIndex:  1,
			DNSMaxCount:    50,
			GetCertParam:   "format",
		},
		Response: PKIResponse{
			Format: "pem",
		},
	}
}

// ============================================================================
// Mock CA Signer - For testing and development
// ============================================================================

// SignRequest represents a signing request to the Mock CA
type SignRequest struct {
	CSR          string `json:"csr"`
	ValidityDays int    `json:"validity_days,omitempty"`
}

// SignResponse represents a signing response from the Mock CA
type SignResponse struct {
	Certificate string `json:"certificate"`
	Chain       string `json:"chain"`
}

// generateRSAKey generates an RSA private key of the specified bit size
func generateRSAKey(bits int) (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, bits)
}

// generateSerialNumber generates a random serial number for certificates
func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialNumberLimit)
}

// MockCASigner implements local self-signing for development and testing
// It generates a CA certificate on first use and signs certificates locally
type MockCASigner struct {
	caCert    *x509.Certificate
	caKey     interface{}
	caPEM     []byte
	caKeyPEM  []byte
	generated bool
}

// NewMockCASigner creates a new self-signing Mock CA
func NewMockCASigner(baseURL string) *MockCASigner {
	// baseURL is ignored for self-signing - kept for API compatibility
	return &MockCASigner{}
}

// ensureCA generates the CA certificate and key if not already done
func (s *MockCASigner) ensureCA() error {
	if s.generated {
		return nil
	}

	// Generate CA private key (RSA 2048)
	caPrivKey, err := generateRSAKey(2048)
	if err != nil {
		return fmt.Errorf("failed to generate CA key: %w", err)
	}

	// Create CA certificate template
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return fmt.Errorf("failed to generate serial: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "External Issuer Mock CA",
			Organization: []string{"cert-manager-external-issuer"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	// Self-sign the CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return fmt.Errorf("failed to create CA certificate: %w", err)
	}

	s.caCert, err = x509.ParseCertificate(caCertDER)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	s.caKey = caPrivKey

	// Encode to PEM
	s.caPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertDER,
	})

	s.caKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caPrivKey),
	})

	s.generated = true
	return nil
}

// CheckHealth verifies the Mock CA is ready
func (s *MockCASigner) CheckHealth() error {
	// For self-signing, we just ensure CA is generated
	if err := s.ensureCA(); err != nil {
		return fmt.Errorf("Mock CA initialization failed: %w", err)
	}
	return nil
}

// Sign signs a CSR using the local Mock CA
func (s *MockCASigner) Sign(csrPEM []byte, validityDays int) ([]byte, []byte, error) {
	// Ensure CA is initialized
	if err := s.ensureCA(); err != nil {
		return nil, nil, fmt.Errorf("CA not ready: %w", err)
	}

	// Parse the CSR
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("invalid CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return nil, nil, fmt.Errorf("CSR signature validation failed: %w", err)
	}

	// Generate serial number
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial: %w", err)
	}

	// Create certificate template
	certTemplate := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               csr.Subject,
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().AddDate(0, 0, validityDays),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              csr.DNSNames,
		IPAddresses:           csr.IPAddresses,
		URIs:                  csr.URIs,
		EmailAddresses:        csr.EmailAddresses,
	}

	// Sign the certificate with our CA
	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, s.caCert, csr.PublicKey, s.caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign certificate: %w", err)
	}

	// Encode to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return certPEM, s.caPEM, nil
}
