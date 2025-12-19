# MockCA Server

The MockCA Server is a standalone HTTP-based Certificate Authority for testing and development. It can be deployed as a Kubernetes pod or run as a standalone console application.

## Features

- **HTTP API**: RESTful API for certificate signing
- **Extensive Logging**: Structured logging with configurable levels (debug, info, warn, error)
- **JSON or Text Logs**: Configurable log format for container environments
- **Health Endpoints**: Kubernetes-compatible health checks
- **Self-contained**: No external dependencies, generates CA on startup
- **Multiple Endpoints**: Supports various API paths for compatibility testing

## Quick Start

### Run Locally (Console)

```bash
# Build and run with debug logging
make build-mockca-local
./bin/mockca-server --log-level=debug

# Or use go run
go run ./cmd/mockca --log-level=debug --log-format=text
```

### Run with Docker

```bash
# Build the image
make docker-build-mockca

# Run with debug logging
docker run -p 8080:8080 \
  -e MOCKCA_LOG_LEVEL=debug \
  -e MOCKCA_LOG_FORMAT=json \
  mockca-server:latest
```

### Deploy to Kubernetes

```bash
# Deploy the MockCA server
kubectl apply -f deploy/mockca-server.yaml

# Check the logs
kubectl logs -n mockca-system -l app.kubernetes.io/name=mockca-server -f
```

## API Endpoints

| Endpoint | Method | Description |
| -------- | ------ | ----------- |
| `/health` | GET | Health check (JSON response) |
| `/healthz` | GET | Kubernetes liveness probe |
| `/readyz` | GET | Kubernetes readiness probe |
| `/ca` | GET | Download CA certificate (PEM) |
| `/sign` | POST | Sign a CSR (JSON format) |
| `/api/v1/sign` | POST | Sign a CSR (JSON alternate path) |
| `/api/v1/certificate/sign` | POST | Sign a CSR (JSON alternate path) |
| `/cgi/pki.cgi` | POST | **Legacy PKI-compatible endpoint** |

## Legacy PKI-Compatible Endpoint

The `/cgi/pki.cgi` endpoint mimics legacy PKI API formats (such as `pki.example.com/cgi/pki.cgi`).

### Request Format

POST with semicolon-separated parameters:

| Parameter | Description |
| --------- | ----------- |
| `new=1` | Create new certificate or return existing |
| `renew=1` | Force recreation of certificate |
| `subject` | Full DN (e.g., `/C=US/ST=California/L=San Francisco/O=Example/CN=example.com`) |
| `getCERT` | Return existing certificate for subject |
| `getKEY` | Return existing private key |
| `getCSR` | Return existing CSR |
| `DNS2`-`DNS20` | Subject Alternative Names |

### Example: Create New Certificate

```bash
# Create a new certificate
curl -s -X POST \
  -d "new=1;subject=/C=US/ST=California/L=San Francisco/O=Example/CN=myapp.example.com;DNS2=myapp2.example.com" \
  http://localhost:8080/cgi/pki.cgi > myapp.example.com.pem

# View the certificate
openssl x509 -in myapp.example.com.pem -text -noout
```

### Example: Force Renewal

```bash
# Force renewal of an existing certificate
curl -s -X POST \
  -d "renew=1;subject=/C=US/ST=California/L=San Francisco/O=Example/CN=myapp.example.com" \
  http://localhost:8080/cgi/pki.cgi > myapp.example.com.pem
```

### Example: Retrieve Existing Certificate

```bash
# Get existing certificate
curl -s -X POST \
  -d "getCERT;subject=/C=US/ST=California/L=San Francisco/O=Example/CN=myapp.example.com" \
  http://localhost:8080/cgi/pki.cgi
```

### Response Format (PKI Endpoint)

Returns raw PEM certificate followed by CA certificate (no JSON wrapper):

```
-----BEGIN CERTIFICATE-----
MIIDxTCCAq2gAwIBAgIRAL...
...certificate content...
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIC9zCCAd+gAwIBAgIRAJ...
...CA certificate content...
-----END CERTIFICATE-----
```

## Sign a Certificate

### Request Format (JSON)

```bash
curl -X POST http://localhost:8080/sign \
  -H "Content-Type: application/json" \
  -d '{
    "csr": "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----",
    "validity_days": 90
  }'
```

### Request Format (Raw PEM)

```bash
curl -X POST http://localhost:8080/sign \
  -H "Content-Type: application/x-pem-file" \
  --data-binary @my-csr.pem
```

### Response Format

```json
{
  "certificate": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
  "certificate_chain": "-----BEGIN CERTIFICATE-----\n...(leaf)...\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\n...(CA)...\n-----END CERTIFICATE-----\n",
  "ca": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
  "serial_number": "123456789...",
  "not_before": "2024-01-01T00:00:00Z",
  "not_after": "2024-04-01T00:00:00Z",
  "subject": "CN=example.com,O=Example Org"
}
```

## Configuration

### Command-Line Flags

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--addr` | `:8080` | Address to listen on |
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--log-format` | `text` | Log format: json, text |
| `--ca-cn` | `External Issuer Mock CA` | CA Common Name |
| `--ca-org` | `cert-manager-external-issuer` | CA Organization |
| `--ca-validity` | `10` | CA validity in years |
| `--cert-validity` | `365` | Default certificate validity in days |

### Environment Variables

| Variable | Description |
| -------- | ----------- |
| `MOCKCA_ADDR` | Override `--addr` |
| `MOCKCA_LOG_LEVEL` | Override `--log-level` |
| `MOCKCA_LOG_FORMAT` | Override `--log-format` |

## Logging Examples

### Info Level (Default)

```
time=2024-01-15T10:30:00.000Z level=INFO msg="Starting Mock CA Server" version=1.0.0 addr=:8080
time=2024-01-15T10:30:00.001Z level=INFO msg="Mock CA initialized successfully" ca_subject="CN=External Issuer Mock CA,O=cert-manager-external-issuer"
time=2024-01-15T10:30:00.002Z level=INFO msg="Mock CA Server is ready" addr=:8080
time=2024-01-15T10:30:05.123Z level=INFO msg="HTTP request" method=POST path=/sign status=200 duration_ms=15
time=2024-01-15T10:30:05.124Z level=INFO msg="Certificate signed successfully" serial=12345 subject="CN=example.com" dns_names=["example.com","www.example.com"]
```

### Debug Level

```
time=2024-01-15T10:30:00.000Z level=DEBUG msg="Generating CA private key" bits=2048
time=2024-01-15T10:30:00.050Z level=DEBUG msg="CA private key generated successfully"
time=2024-01-15T10:30:00.051Z level=DEBUG msg="CA serial number generated" serial=12345...
time=2024-01-15T10:30:00.052Z level=DEBUG msg="Creating CA certificate" subject="CN=..." not_before=... not_after=...
time=2024-01-15T10:30:05.100Z level=DEBUG msg="Certificate signing request received" content_type=application/json content_length=1234
time=2024-01-15T10:30:05.101Z level=DEBUG msg="Request body received" size=1234
time=2024-01-15T10:30:05.102Z level=DEBUG msg="CSR received" csr_length=1100
time=2024-01-15T10:30:05.103Z level=INFO msg="CSR parsed successfully" subject="CN=example.com" dns_names=["example.com"]
time=2024-01-15T10:30:05.104Z level=DEBUG msg="Creating certificate" serial=67890 validity_days=90
time=2024-01-15T10:30:05.120Z level=INFO msg="Certificate signed successfully" serial=67890 subject="CN=example.com"
```

### JSON Format

```json
{"time":"2024-01-15T10:30:05.123Z","level":"INFO","msg":"Certificate signed successfully","serial":"67890","subject":"CN=example.com","dns_names":["example.com"],"total_signed":42}
```

## Use with External Issuer

To configure the External Issuer controller to use the standalone MockCA server:

1. Deploy the MockCA server:
   ```bash
   kubectl apply -f deploy/mockca-server.yaml
   ```

2. Create a ConfigMap with PKI configuration:
   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: mockca-pki-config
     namespace: external-issuer-system
   data:
     pki-config.json: |
       {
         "baseUrl": "http://mockca-server.mockca-system.svc.cluster.local:8080/api/v1/sign",
         "method": "POST",
         "response": {
           "format": "json",
           "certificateField": "certificate",
           "chainField": "certificate_chain"
         }
       }
   ```

3. Create a ClusterIssuer that uses the PKI signer:
   ```yaml
   apiVersion: external-issuer.io/v1alpha1
   kind: ExternalClusterIssuer
   metadata:
     name: mockca-pki-issuer
   spec:
     signerType: pki
     configMapRef:
       name: mockca-pki-config
       namespace: external-issuer-system
   ```

## Testing with curl

```bash
# Generate a test CSR
openssl req -new -newkey rsa:2048 -nodes -keyout test.key -out test.csr \
  -subj "/CN=test.example.com/O=Test Org"

# Sign the CSR
curl -X POST http://localhost:8080/sign \
  -H "Content-Type: application/x-pem-file" \
  --data-binary @test.csr | jq .

# Get the CA certificate
curl http://localhost:8080/ca -o ca.crt

# Verify the signed certificate
openssl verify -CAfile ca.crt signed.crt
```

## Health Check

```bash
curl http://localhost:8080/health | jq .
```

Response:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "ca_subject": "CN=External Issuer Mock CA,O=cert-manager-external-issuer",
  "ca_expires": "2034-01-15T10:30:00Z",
  "certificates_signed": 42,
  "uptime": "2h30m15s"
}
```
