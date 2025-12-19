# End-to-End Testing Plan

## Overview

This document outlines the complete end-to-end testing process for the cert-manager External Issuer:
1. **Local Docker Build** - Build both controller and MockCA images locally
2. **Push to Docker Hub** - Tag and push images to Docker Hub registry
3. **Deploy to AKS** - Pull images from Docker Hub and deploy to Azure Kubernetes Service
4. **Verify Integration** - Test certificate issuance with MockCA as external CA

---

## Prerequisites

### Local Environment
- [ ] Docker Desktop installed and running
- [ ] PowerShell 7+ or bash terminal
- [ ] kubectl CLI installed
- [ ] Go 1.24+ (for local development/testing)

### Azure/AKS
- [ ] Azure CLI installed (`az --version`)
- [ ] Logged into Azure (`az login`)
- [ ] AKS cluster available with kubectl access
- [ ] cert-manager installed on AKS cluster

### Docker Hub
- [ ] Docker Hub account created
- [ ] Docker logged in (`docker login`)

---

## Phase 1: Local Docker Build

### Step 1.1: Build External Issuer Controller Image

```powershell
# Navigate to project root
cd <your-project-directory>

# Build the controller image
docker build -t external-issuer:latest .

# Verify the image was created
docker images | Select-String "external-issuer"
```

### Step 1.2: Build MockCA Server Image

```powershell
# Build the MockCA server image
docker build -f Dockerfile.mockca -t mockca-server:latest .

# Verify the image was created
docker images | Select-String "mockca-server"
```

### Step 1.3: Local Smoke Test (Optional)

```powershell
# Test MockCA server locally
docker run -d --name mockca-test -p 8080:8080 mockca-server:latest

# Verify it's running
curl http://localhost:8080/healthz
# Expected: {"status":"ok"}

# Check signing endpoint
curl http://localhost:8080/api/v1/sign -X POST -H "Content-Type: application/json" -d '{"common_name":"test.example.com"}'

# Cleanup
docker stop mockca-test && docker rm mockca-test
```

---

## Phase 2: Push to Docker Hub

### Step 2.1: Configure Docker Hub Registry

```powershell
# Set your Docker Hub username (replace with your actual username)
$DOCKER_HUB_USER = "your-dockerhub-username"

# Or use environment variable
$DOCKER_HUB_USER = $env:DOCKER_HUB_USER
```

### Step 2.2: Tag Images for Docker Hub

```powershell
# Tag controller image
docker tag external-issuer:latest ${DOCKER_HUB_USER}/external-issuer:latest
docker tag external-issuer:latest ${DOCKER_HUB_USER}/external-issuer:v1.0.0

# Tag MockCA image
docker tag mockca-server:latest ${DOCKER_HUB_USER}/mockca-server:latest
docker tag mockca-server:latest ${DOCKER_HUB_USER}/mockca-server:v1.0.0

# Verify tags
docker images | Select-String "${DOCKER_HUB_USER}"
```

### Step 2.3: Push to Docker Hub

```powershell
# Login to Docker Hub (if not already)
docker login

# Push controller images
docker push ${DOCKER_HUB_USER}/external-issuer:latest
docker push ${DOCKER_HUB_USER}/external-issuer:v1.0.0

# Push MockCA images
docker push ${DOCKER_HUB_USER}/mockca-server:latest
docker push ${DOCKER_HUB_USER}/mockca-server:v1.0.0

# Verify on Docker Hub
Write-Host "Verify at: https://hub.docker.com/r/${DOCKER_HUB_USER}/external-issuer"
Write-Host "Verify at: https://hub.docker.com/r/${DOCKER_HUB_USER}/mockca-server"
```

---

## Phase 3: Deploy to AKS

### Step 3.1: Connect to AKS Cluster

```powershell
# Get AKS credentials (adjust names for your environment)
$RESOURCE_GROUP = "your-resource-group"
$AKS_CLUSTER = "your-aks-cluster"

az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_CLUSTER --overwrite-existing

# Verify connection
kubectl cluster-info
kubectl get nodes
```

### Step 3.2: Verify cert-manager is Installed

```powershell
# Check cert-manager is running
kubectl get pods -n cert-manager

# If not installed, install it:
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=300s
kubectl wait --for=condition=Available deployment/cert-manager-webhook -n cert-manager --timeout=300s
kubectl wait --for=condition=Available deployment/cert-manager-cainjector -n cert-manager --timeout=300s
```

### Step 3.3: Deploy MockCA Server (Separate Pod)

```powershell
# Create namespace
kubectl create namespace mockca-system --dry-run=client -o yaml | kubectl apply -f -

# Update mockca-server.yaml to use Docker Hub image (see Phase 3.5 for script)
# Apply MockCA deployment
kubectl apply -f deploy/mockca-server.yaml

# Wait for MockCA to be ready
kubectl wait --for=condition=Available deployment/mockca-server -n mockca-system --timeout=120s

# Verify MockCA is running
kubectl get pods -n mockca-system
kubectl logs -n mockca-system -l app.kubernetes.io/name=mockca-server --tail=20
```

### Step 3.4: Deploy External Issuer Controller

```powershell
# Install CRDs
kubectl apply -f deploy/crds/

# Install RBAC
kubectl apply -f deploy/rbac/

# Install PKI configuration (pointing to MockCA)
kubectl apply -f deploy/config/pki-config.yaml

# Update deployment.yaml to use Docker Hub image (see Phase 3.5 for script)
# Apply controller deployment  
kubectl apply -f deploy/deployment.yaml

# Wait for controller to be ready
kubectl wait --for=condition=Available deployment/external-issuer-controller -n external-issuer-system --timeout=120s

# Verify controller is running
kubectl get pods -n external-issuer-system
kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer --tail=20
```

### Step 3.5: Automated Deployment Script

Create a deployment script that updates image references and deploys:

```powershell
# File: scripts/e2e-deploy-dockerhub.ps1
# See the script created alongside this plan
```

---

## Phase 4: Configure and Test

### Step 4.1: Create ExternalClusterIssuer

```powershell
# Deploy the cluster issuer (uses MockCA)
kubectl apply -f deploy/issuer/cluster-issuer.yaml

# Verify issuer status
kubectl get externalclusterissuers
kubectl describe externalclusterissuer pki-cluster-issuer
```

### Step 4.2: Create Test Certificate

```powershell
# Deploy a basic test certificate
kubectl apply -f examples/basic-certificate.yaml

# Wait for certificate to be ready
kubectl wait --for=condition=Ready certificate/example-app-tls -n default --timeout=120s

# Verify certificate was issued
kubectl get certificate example-app-tls -n default
kubectl describe certificate example-app-tls -n default

# Check the secret was created with certificate data
kubectl get secret example-app-tls -n default -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
```

### Step 4.3: Verify Full Chain

```powershell
# Check controller processed the request
kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer --tail=50 | Select-String "Signed"

# Check MockCA signed the certificate
kubectl logs -n mockca-system -l app.kubernetes.io/name=mockca-server --tail=50 | Select-String "sign"

# Verify certificate chain
$cert = kubectl get secret example-app-tls -n default -o jsonpath='{.data.tls\.crt}' | base64 -d
$cert | openssl x509 -text -noout | Select-String "Subject:|Issuer:|Not Before:|Not After"
```

---

## Phase 5: Cleanup

### Step 5.1: Remove Test Resources

```powershell
# Remove test certificate and secret
kubectl delete certificate example-app-tls -n default
kubectl delete secret example-app-tls -n default

# Remove cluster issuer
kubectl delete externalclusterissuer pki-cluster-issuer
```

### Step 5.2: Remove Deployments (Optional)

```powershell
# Remove controller
kubectl delete -f deploy/deployment.yaml
kubectl delete -f deploy/config/pki-config.yaml
kubectl delete -f deploy/rbac/
kubectl delete -f deploy/crds/

# Remove MockCA
kubectl delete -f deploy/mockca-server.yaml
```

---

## Troubleshooting

### Common Issues

#### 1. ImagePullBackOff
```powershell
# Check image pull errors
kubectl describe pod -n external-issuer-system -l app.kubernetes.io/name=external-issuer

# Verify Docker Hub image is public or create pull secret
kubectl create secret docker-registry dockerhub-secret \
  --docker-server=https://index.docker.io/v1/ \
  --docker-username=$DOCKER_HUB_USER \
  --docker-password=$DOCKER_HUB_TOKEN \
  -n external-issuer-system
```

#### 2. Certificate Not Ready
```powershell
# Check CertificateRequest status
kubectl get certificaterequests -A
kubectl describe certificaterequest <name> -n <namespace>

# Check controller logs
kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer -f

# Check MockCA connectivity from controller pod
kubectl exec -n external-issuer-system deploy/external-issuer-controller -- \
  wget -qO- http://mockca-server.mockca-system.svc.cluster.local:8080/healthz
```

#### 3. Webhook Denied (cert-manager webhook issue)
```powershell
# Restart cert-manager webhook
kubectl rollout restart deployment cert-manager-webhook -n cert-manager
kubectl wait --for=condition=Available deployment/cert-manager-webhook -n cert-manager --timeout=120s
```

---

## Notes for Other Team's Error

The error mentioned in the request is related to **Let's Encrypt ACME issuer with Azure DNS**, not this External Issuer. The key issues are:

1. **Missing `clientID`**: The ACME ClusterIssuer requires Azure DNS credentials:
   ```yaml
   spec:
     acme:
       solvers:
       - dns01:
           azureDNS:
             clientID: "<azure-app-registration-client-id>"  # REQUIRED
             clientSecretSecretRef:
               name: azure-dns-secret
               key: client-secret
             subscriptionID: "<subscription-id>"
             tenantID: "<tenant-id>"
             resourceGroupName: "<dns-zone-resource-group>"
             hostedZoneName: "<your-domain.com>"
   ```

2. **Alternative: Managed Identity** (if using AKS with managed identity):
   ```yaml
   spec:
     acme:
       solvers:
       - dns01:
           azureDNS:
             managedIdentity:
               clientID: "<managed-identity-client-id>"
             subscriptionID: "<subscription-id>"
             resourceGroupName: "<dns-zone-resource-group>"
             hostedZoneName: "<your-domain.com>"
   ```

See the separate troubleshooting document for Let's Encrypt Azure DNS configuration.

---

## Quick Reference Commands

```powershell
# Build all images
make docker-build-all

# Push to Docker Hub
$DH="your-username"; docker tag external-issuer:latest $DH/external-issuer:latest; docker push $DH/external-issuer:latest

# Deploy all
make deploy-all

# Check status
make verify

# View logs
make logs
make logs-mockca

# Cleanup
make clean-all
```
