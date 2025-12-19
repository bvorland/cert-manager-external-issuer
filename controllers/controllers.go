package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	externalissuerapi "github.com/bvorland/cert-manager-external-issuer/api/v1alpha1"
	"github.com/bvorland/cert-manager-external-issuer/internal/signer"
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	issuerReadyCondition   = "Ready"
	externalIssuerAPIGroup = "external-issuer.io"
	issuerKind             = "ExternalIssuer"
	clusterIssuerKind      = "ExternalClusterIssuer"
	defaultConfigKey       = "pki-config.json"
	defaultNamespace       = "external-issuer-system"
)

// Signer interface for certificate signing
type Signer interface {
	CheckHealth() error
	Sign(csrPEM []byte, validityDays int) (certPEM []byte, caPEM []byte, err error)
}

// CertificateRequestReconciler reconciles CertificateRequest objects
type CertificateRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cert-manager.io,resources=certificaterequests,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificaterequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=external-issuer.io,resources=externalissuers;externalclusterissuers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;list;watch

func (r *CertificateRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the CertificateRequest
	cr := &cmapi.CertificateRequest{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if this CertificateRequest is for our issuer type
	if cr.Spec.IssuerRef.Group != externalIssuerAPIGroup {
		return ctrl.Result{}, nil
	}

	if cr.Spec.IssuerRef.Kind != issuerKind && cr.Spec.IssuerRef.Kind != clusterIssuerKind {
		return ctrl.Result{}, nil
	}

	// Skip if already has a certificate or is in a terminal state
	if len(cr.Status.Certificate) > 0 {
		return ctrl.Result{}, nil
	}

	if isInTerminalState(cr) {
		return ctrl.Result{}, nil
	}

	// Check if the CertificateRequest has been denied
	// If denied, we should not process it - this is a terminal state
	if isCertificateRequestDenied(cr) {
		logger.Info("CertificateRequest has been denied, skipping", "name", cr.Name)
		return ctrl.Result{}, nil
	}

	// Check if the CertificateRequest has been approved
	// Approval should be handled by cert-manager's internal approver or approver-policy.
	// The approver-clusterrole.yaml grants cert-manager permission to approve our issuer types.
	// See: https://cert-manager.io/docs/usage/certificaterequest/#approval
	if !isCertificateRequestApproved(cr) {
		logger.Info("CertificateRequest not yet approved, waiting for approval", "name", cr.Name)
		// Return without error - the controller will be notified when the CR is updated
		return ctrl.Result{}, nil
	}

	logger.Info("Processing CertificateRequest", "name", cr.Name, "issuer", cr.Spec.IssuerRef.Name)

	// Get the issuer spec
	issuerSpec, err := r.getIssuerSpec(ctx, cr)
	if err != nil {
		logger.Error(err, "Failed to get issuer")
		return ctrl.Result{}, r.setStatus(ctx, cr, cmmeta.ConditionFalse, "IssuerNotFound", err.Error())
	}

	// Create the appropriate signer based on configuration
	var certSigner Signer
	signerType := issuerSpec.SignerType
	if signerType == "" {
		signerType = "mockca" // Default for backward compatibility
	}

	if signerType == "pki" && issuerSpec.ConfigMapRef != nil {
		// Load PKI configuration from ConfigMap
		pkiConfig, err := r.loadPKIConfig(ctx, issuerSpec.ConfigMapRef, cr.Namespace)
		if err != nil {
			logger.Error(err, "Failed to load PKI config")
			return ctrl.Result{}, r.setStatus(ctx, cr, cmmeta.ConditionFalse, "ConfigError", err.Error())
		}
		pkiSigner := signer.NewPKISigner(pkiConfig)

		// Load auth token if specified
		if issuerSpec.AuthSecretName != "" {
			token, err := r.loadAuthToken(ctx, issuerSpec.AuthSecretName, cr.Namespace)
			if err != nil {
				logger.Error(err, "Failed to load auth token")
				return ctrl.Result{}, r.setStatus(ctx, cr, cmmeta.ConditionFalse, "AuthError", err.Error())
			}
			pkiSigner.SetAuthToken(token)
		}
		certSigner = pkiSigner
	} else {
		// Use Mock CA signer (default)
		certSigner = signer.NewMockCASigner(issuerSpec.URL)
	}

	// Check health first
	if err := certSigner.CheckHealth(); err != nil {
		logger.Error(err, "CA health check failed")
		return ctrl.Result{}, r.setStatus(ctx, cr, cmmeta.ConditionFalse, "SignerError", err.Error())
	}

	// Sign the CSR
	certPEM, caPEM, err := certSigner.Sign(cr.Spec.Request, 365)
	if err != nil {
		logger.Error(err, "Failed to sign certificate")
		return ctrl.Result{}, r.setStatus(ctx, cr, cmmeta.ConditionFalse, "SigningFailed", err.Error())
	}

	logger.Info("Successfully signed certificate", "name", cr.Name)

	// Update the CertificateRequest with the signed certificate
	cr.Status.Certificate = certPEM
	cr.Status.CA = caPEM

	return ctrl.Result{}, r.setStatus(ctx, cr, cmmeta.ConditionTrue, "Issued", "Certificate issued successfully")
}

func (r *CertificateRequestReconciler) getIssuerSpec(ctx context.Context, cr *cmapi.CertificateRequest) (*externalissuerapi.ExternalIssuerSpec, error) {
	if cr.Spec.IssuerRef.Kind == clusterIssuerKind {
		// Get ClusterIssuer
		clusterIssuer := &externalissuerapi.ExternalClusterIssuer{}
		if err := r.Get(ctx, types.NamespacedName{Name: cr.Spec.IssuerRef.Name}, clusterIssuer); err != nil {
			return nil, fmt.Errorf("failed to get ClusterIssuer %s: %w", cr.Spec.IssuerRef.Name, err)
		}
		// Check if issuer is ready
		if !isIssuerReady(clusterIssuer.Status.Conditions) {
			return nil, fmt.Errorf("clusterIssuer %s is not ready", cr.Spec.IssuerRef.Name)
		}
		return &clusterIssuer.Spec, nil
	}

	// Get namespaced Issuer
	issuer := &externalissuerapi.ExternalIssuer{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Spec.IssuerRef.Name, Namespace: cr.Namespace}, issuer); err != nil {
		return nil, fmt.Errorf("failed to get Issuer %s/%s: %w", cr.Namespace, cr.Spec.IssuerRef.Name, err)
	}
	// Check if issuer is ready
	if !isIssuerReady(issuer.Status.Conditions) {
		return nil, fmt.Errorf("issuer %s/%s is not ready", cr.Namespace, cr.Spec.IssuerRef.Name)
	}
	return &issuer.Spec, nil
}

func (r *CertificateRequestReconciler) setStatus(ctx context.Context, cr *cmapi.CertificateRequest, status cmmeta.ConditionStatus, reason, message string) error {
	cr.Status.Conditions = setCondition(cr.Status.Conditions, cmapi.CertificateRequestCondition{
		Type:               cmapi.CertificateRequestConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	})
	return r.Status().Update(ctx, cr)
}

func setCondition(conditions []cmapi.CertificateRequestCondition, condition cmapi.CertificateRequestCondition) []cmapi.CertificateRequestCondition {
	for i, c := range conditions {
		if c.Type == condition.Type {
			conditions[i] = condition
			return conditions
		}
	}
	return append(conditions, condition)
}

func isInTerminalState(cr *cmapi.CertificateRequest) bool {
	for _, c := range cr.Status.Conditions {
		if c.Type == cmapi.CertificateRequestConditionReady {
			if c.Status == cmmeta.ConditionTrue || c.Reason == cmapi.CertificateRequestReasonFailed || c.Reason == cmapi.CertificateRequestReasonDenied {
				return true
			}
		}
	}
	return false
}

func isCertificateRequestApproved(cr *cmapi.CertificateRequest) bool {
	for _, c := range cr.Status.Conditions {
		if c.Type == cmapi.CertificateRequestConditionApproved && c.Status == cmmeta.ConditionTrue {
			return true
		}
	}
	return false
}

func isCertificateRequestDenied(cr *cmapi.CertificateRequest) bool {
	for _, c := range cr.Status.Conditions {
		if c.Type == cmapi.CertificateRequestConditionDenied && c.Status == cmmeta.ConditionTrue {
			return true
		}
	}
	return false
}

func isIssuerReady(conditions []metav1.Condition) bool {
	for _, c := range conditions {
		if c.Type == issuerReadyCondition && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *CertificateRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cmapi.CertificateRequest{}).
		Complete(r)
}

// loadPKIConfig loads PKI configuration from a ConfigMap
func (r *CertificateRequestReconciler) loadPKIConfig(ctx context.Context, ref *externalissuerapi.ConfigMapReference, requestNamespace string) (*signer.PKIConfig, error) {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = requestNamespace
	}
	if namespace == "" {
		namespace = defaultNamespace
	}

	key := ref.Key
	if key == "" {
		key = defaultConfigKey
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, ref.Name, err)
	}

	configData, ok := cm.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in ConfigMap %s/%s", key, namespace, ref.Name)
	}

	var config signer.PKIConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse PKI config: %w", err)
	}

	return &config, nil
}

// loadAuthToken loads an authentication token from a Secret
func (r *CertificateRequestReconciler) loadAuthToken(ctx context.Context, secretName, namespace string) (string, error) {
	if namespace == "" {
		namespace = defaultNamespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	// Try common key names
	for _, key := range []string{"token", "api-key", "password", "apiKey"} {
		if token, ok := secret.Data[key]; ok {
			return string(token), nil
		}
	}

	return "", fmt.Errorf("no token found in secret %s/%s (tried: token, api-key, password, apiKey)", namespace, secretName)
}

// IssuerReconciler reconciles ExternalIssuer objects
type IssuerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=external-issuer.io,resources=externalissuers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=external-issuer.io,resources=externalissuers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *IssuerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	issuer := &externalissuerapi.ExternalIssuer{}
	if err := r.Get(ctx, req.NamespacedName, issuer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ExternalIssuer", "name", issuer.Name, "namespace", issuer.Namespace)

	// Determine signer type and check health
	var err error
	signerType := issuer.Spec.SignerType
	if signerType == "" {
		signerType = "mockca"
	}

	if signerType == "pki" && issuer.Spec.ConfigMapRef != nil {
		pkiConfig, loadErr := r.loadPKIConfigForIssuer(ctx, issuer.Spec.ConfigMapRef, issuer.Namespace)
		if loadErr != nil {
			err = loadErr
		} else {
			pkiSigner := signer.NewPKISigner(pkiConfig)
			err = pkiSigner.CheckHealth()
		}
	} else {
		mockSigner := signer.NewMockCASigner(issuer.Spec.URL)
		err = mockSigner.CheckHealth()
	}

	condition := metav1.Condition{
		Type:               issuerReadyCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: issuer.Generation,
	}

	if err != nil {
		logger.Error(err, "CA health check failed")
		condition.Status = metav1.ConditionFalse
		condition.Reason = "HealthCheckFailed"
		condition.Message = err.Error()
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Success"
		condition.Message = fmt.Sprintf("%s CA is healthy and ready", signerType)
	}

	meta.SetStatusCondition(&issuer.Status.Conditions, condition)
	if updateErr := r.Status().Update(ctx, issuer); updateErr != nil {
		return ctrl.Result{}, updateErr
	}

	return ctrl.Result{}, nil
}

func (r *IssuerReconciler) loadPKIConfigForIssuer(ctx context.Context, ref *externalissuerapi.ConfigMapReference, defaultNs string) (*signer.PKIConfig, error) {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = defaultNs
	}
	key := ref.Key
	if key == "" {
		key = "pki-config.json"
	}
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, ref.Name, err)
	}
	configData, ok := cm.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in ConfigMap", key)
	}
	var config signer.PKIConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse PKI config: %w", err)
	}
	return &config, nil
}

func (r *IssuerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&externalissuerapi.ExternalIssuer{}).
		Complete(r)
}

// ClusterIssuerReconciler reconciles ExternalClusterIssuer objects
type ClusterIssuerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=external-issuer.io,resources=externalclusterissuers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=external-issuer.io,resources=externalclusterissuers/status,verbs=get;update;patch

func (r *ClusterIssuerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	issuer := &externalissuerapi.ExternalClusterIssuer{}
	if err := r.Get(ctx, req.NamespacedName, issuer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ExternalClusterIssuer", "name", issuer.Name)

	// Determine signer type and check health
	var err error
	signerType := issuer.Spec.SignerType
	if signerType == "" {
		signerType = "mockca"
	}

	if signerType == "pki" && issuer.Spec.ConfigMapRef != nil {
		pkiConfig, loadErr := r.loadPKIConfigForClusterIssuer(ctx, issuer.Spec.ConfigMapRef)
		if loadErr != nil {
			err = loadErr
		} else {
			pkiSigner := signer.NewPKISigner(pkiConfig)
			err = pkiSigner.CheckHealth()
		}
	} else {
		mockSigner := signer.NewMockCASigner(issuer.Spec.URL)
		err = mockSigner.CheckHealth()
	}

	condition := metav1.Condition{
		Type:               issuerReadyCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: issuer.Generation,
	}

	if err != nil {
		logger.Error(err, "CA health check failed")
		condition.Status = metav1.ConditionFalse
		condition.Reason = "HealthCheckFailed"
		condition.Message = err.Error()
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Success"
		condition.Message = fmt.Sprintf("%s CA is healthy and ready", signerType)
	}

	meta.SetStatusCondition(&issuer.Status.Conditions, condition)
	if updateErr := r.Status().Update(ctx, issuer); updateErr != nil {
		return ctrl.Result{}, updateErr
	}

	return ctrl.Result{}, nil
}

func (r *ClusterIssuerReconciler) loadPKIConfigForClusterIssuer(ctx context.Context, ref *externalissuerapi.ConfigMapReference) (*signer.PKIConfig, error) {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	key := ref.Key
	if key == "" {
		key = "pki-config.json"
	}
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, ref.Name, err)
	}
	configData, ok := cm.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in ConfigMap", key)
	}
	var config signer.PKIConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse PKI config: %w", err)
	}
	return &config, nil
}

func (r *ClusterIssuerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&externalissuerapi.ExternalClusterIssuer{}).
		Complete(r)
}
