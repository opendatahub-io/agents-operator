/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bootstrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kagenti/operator/internal/mlflow"
)

const (
	ingressCAConfigMap     = "otel-ingress-ca"
	ingressCertConfigMap   = "default-ingress-cert"
	ingressCertNamespace   = "openshift-config-managed"
	rootCAConfigMap        = "kube-root-ca.crt"
	rootCANamespace        = "openshift-config"
	collectorConfigMapName = "otel-collector-config"
	collectorDeployment    = "otel-collector"
	phoenixServiceName     = "phoenix"

	configMapDataKey  = "base.yaml"
	caBundleKey       = "ca-bundle.crt"
	rootCAKey         = "ca.crt"
	restartAnnotation = "kagenti.io/otel-config-hash"
	ocpAPIGroup       = "config.openshift.io"
	mlflowCRDGroup    = "mlflow.opendatahub.io"
	mlflowCRDVersion  = "v1"
	mlflowCRDResource = "mlflows"

	defaultBackoffInitial = 5 * time.Second
	defaultBackoffMax     = 30 * time.Second
	defaultBackoffTimeout = 5 * time.Minute
)

// OtelBootstrapRunnable implements manager.Runnable. It runs once at operator
// startup to bootstrap the OTel collector infrastructure:
//   - Step 1: Project the OpenShift ingress CA into the operator namespace (OCP only).
//   - Step 2: Assemble the OTel collector ConfigMap from component-specific presets.
type OtelBootstrapRunnable struct {
	Client    client.Client
	APIReader client.Reader
	Config    *rest.Config
	Namespace string
	Log       logr.Logger

	// IsOpenShift overrides OCP detection when non-nil (for testing).
	IsOpenShift func(ctx context.Context) (bool, error)
	// MLflowCRDExists overrides CRD discovery when non-nil (for testing).
	MLflowCRDExists func(ctx context.Context) (bool, error)
}

// Start runs the bootstrap sequence. Called by the manager after leader election
// and cache sync, before controllers start processing events.
func (r *OtelBootstrapRunnable) Start(ctx context.Context) error {
	log := r.Log.WithName("otel-bootstrap")
	log.Info("Starting OTel collector bootstrap")

	isOCP, err := r.detectOpenShift(ctx)
	if err != nil {
		return fmt.Errorf("detecting OpenShift: %w", err)
	}

	if isOCP {
		log.Info("OpenShift detected, reconciling ingress CA trust")
		if err := r.reconcileIngressCA(ctx, log); err != nil {
			return fmt.Errorf("ingress CA bootstrap: %w", err)
		}
	} else {
		log.Info("Not running on OpenShift, skipping ingress CA trust")
	}

	if err := r.reconcileCollectorConfig(ctx, log, isOCP); err != nil {
		return fmt.Errorf("collector config bootstrap: %w", err)
	}

	log.Info("OTel collector bootstrap complete")
	return nil
}

// NeedLeaderElection returns true so the bootstrap only runs on the leader.
func (r *OtelBootstrapRunnable) NeedLeaderElection() bool {
	return true
}

// detectOpenShift checks for the config.openshift.io API group.
func (r *OtelBootstrapRunnable) detectOpenShift(ctx context.Context) (bool, error) {
	if r.IsOpenShift != nil {
		return r.IsOpenShift(ctx)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(r.Config)
	if err != nil {
		return false, fmt.Errorf("creating discovery client: %w", err)
	}

	_, apiLists, err := dc.ServerGroupsAndResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return false, fmt.Errorf("discovering API groups: %w", err)
	}

	for _, list := range apiLists {
		gv, parseErr := parseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			continue
		}
		if gv == ocpAPIGroup {
			return true, nil
		}
	}
	return false, nil
}

// reconcileIngressCA reads the OpenShift ingress CA and root CA, then creates
// or updates the otel-ingress-ca ConfigMap in the operator namespace.
func (r *OtelBootstrapRunnable) reconcileIngressCA(ctx context.Context, log logr.Logger) error {
	ingressCert := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: ingressCertConfigMap, Namespace: ingressCertNamespace}
	if err := r.APIReader.Get(ctx, key, ingressCert); err != nil {
		return fmt.Errorf("reading %s/%s: %w", ingressCertNamespace, ingressCertConfigMap, err)
	}

	caBundle, ok := ingressCert.Data[caBundleKey]
	if !ok || caBundle == "" {
		return fmt.Errorf("ConfigMap %s/%s has no %q key", ingressCertNamespace, ingressCertConfigMap, caBundleKey)
	}

	rootCA := &corev1.ConfigMap{}
	rootKey := types.NamespacedName{Name: rootCAConfigMap, Namespace: rootCANamespace}
	if err := r.APIReader.Get(ctx, rootKey, rootCA); err != nil {
		if !errors.IsNotFound(err) {
			log.Info("Could not read root CA ConfigMap, continuing with ingress CA only",
				"error", err, "configmap", rootCAConfigMap, "namespace", rootCANamespace)
		}
	} else if rootCert, ok := rootCA.Data[rootCAKey]; ok && rootCert != "" {
		log.Info("Adding root CA to bundle for full certificate chain")
		caBundle = caBundle + "\n" + rootCert
	}

	existing := &corev1.ConfigMap{}
	existingKey := types.NamespacedName{Name: ingressCAConfigMap, Namespace: r.Namespace}
	if err := r.APIReader.Get(ctx, existingKey, existing); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("checking existing %s ConfigMap: %w", ingressCAConfigMap, err)
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ingressCAConfigMap,
				Namespace: r.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kagenti-operator",
					"app.kubernetes.io/component":  "otel-bootstrap",
				},
			},
			Data: map[string]string{caBundleKey: caBundle},
		}
		if err := r.Client.Create(ctx, cm); err != nil {
			if !errors.IsAlreadyExists(err) {
				return fmt.Errorf("creating %s ConfigMap: %w", ingressCAConfigMap, err)
			}
			log.Info("ConfigMap appeared between check and create, will update", "name", ingressCAConfigMap)
			if err := r.APIReader.Get(ctx, existingKey, existing); err != nil {
				return fmt.Errorf("re-reading %s ConfigMap: %w", ingressCAConfigMap, err)
			}
		} else {
			log.Info("Created ingress CA ConfigMap", "name", ingressCAConfigMap)
			return nil
		}
	}

	if existing.Data[caBundleKey] == caBundle {
		log.Info("Ingress CA ConfigMap already up-to-date, skipping")
		return nil
	}

	existing.Data = map[string]string{caBundleKey: caBundle}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels["app.kubernetes.io/managed-by"] = "kagenti-operator"
	existing.Labels["app.kubernetes.io/component"] = "otel-bootstrap"
	if err := r.Client.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating %s ConfigMap: %w", ingressCAConfigMap, err)
	}
	log.Info("Updated ingress CA ConfigMap", "name", ingressCAConfigMap)
	return nil
}

// reconcileCollectorConfig discovers available components and assembles the
// OTel collector ConfigMap from preset configurations.
func (r *OtelBootstrapRunnable) reconcileCollectorConfig(ctx context.Context, log logr.Logger, isOCP bool) error {
	mf, err := r.discoverMLflow(ctx, log)
	if err != nil {
		return err
	}

	phoenixAvailable := r.discoverPhoenix(ctx, log)

	config, err := assembleCollectorConfig(isOCP, mf, phoenixAvailable)
	if err != nil {
		return fmt.Errorf("assembling collector config: %w", err)
	}

	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshalling collector config: %w", err)
	}

	configStr := string(configYAML)
	configHash := fmt.Sprintf("%x", sha256.Sum256(configYAML))

	existing := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: collectorConfigMapName, Namespace: r.Namespace}
	if err := r.APIReader.Get(ctx, key, existing); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("checking existing collector ConfigMap: %w", err)
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      collectorConfigMapName,
				Namespace: r.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kagenti-operator",
					"app.kubernetes.io/component":  "otel-bootstrap",
				},
				Annotations: map[string]string{
					restartAnnotation: configHash,
				},
			},
			Data: map[string]string{configMapDataKey: configStr},
		}
		if err := r.Client.Create(ctx, cm); err != nil {
			if !errors.IsAlreadyExists(err) {
				return fmt.Errorf("creating collector ConfigMap: %w", err)
			}
			log.Info("ConfigMap appeared between check and create, will update", "name", collectorConfigMapName)
			if err := r.APIReader.Get(ctx, key, existing); err != nil {
				return fmt.Errorf("re-reading collector ConfigMap: %w", err)
			}
		} else {
			log.Info("Created OTel collector ConfigMap", "components",
				componentSummary(mf.available, phoenixAvailable))
			return r.rolloutRestartCollector(ctx, log, configHash)
		}
	}

	existingHash := existing.Annotations[restartAnnotation]
	if existingHash == configHash {
		log.Info("OTel collector ConfigMap unchanged, skipping restart")
		return nil
	}

	existing.Data = map[string]string{configMapDataKey: configStr}
	if existing.Annotations == nil {
		existing.Annotations = make(map[string]string)
	}
	existing.Annotations[restartAnnotation] = configHash
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels["app.kubernetes.io/managed-by"] = "kagenti-operator"
	existing.Labels["app.kubernetes.io/component"] = "otel-bootstrap"
	if err := r.Client.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating collector ConfigMap: %w", err)
	}
	log.Info("Updated OTel collector ConfigMap", "components",
		componentSummary(mf.available, phoenixAvailable))

	return r.rolloutRestartCollector(ctx, log, configHash)
}

// mlflowInfo holds the discovered MLflow endpoint and workspace namespace.
type mlflowInfo struct {
	available   bool
	tracesURL   string // in-cluster traces endpoint (e.g. https://mlflow.ns.svc:8443/v1/traces)
	workspaceNS string // namespace for x-mlflow-workspace header
}

// discoverMLflow checks for the MLflow CRD and, if present, discovers the
// MLflow CR to derive the in-cluster endpoint and workspace namespace.
func (r *OtelBootstrapRunnable) discoverMLflow(ctx context.Context, log logr.Logger) (*mlflowInfo, error) {
	crdExists, err := r.mlflowCRDPresent(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking MLflow CRD: %w", err)
	}
	if !crdExists {
		log.Info("MLflow CRD (mlflows.mlflow.opendatahub.io) not found. " +
			"MLflow presets will be skipped. If the MLflow operator is installed later, " +
			"restart the kagenti-operator pod to pick up MLflow configuration.")
		return &mlflowInfo{}, nil
	}

	log.Info("MLflow CRD detected, discovering MLflow CR")

	list := &mlflow.MLflowList{}
	if err := r.APIReader.List(ctx, list); err != nil {
		log.Info("Could not list MLflow CRs, skipping MLflow presets", "error", err)
		return &mlflowInfo{}, nil
	}

	for i := range list.Items {
		cr := &list.Items[i]
		if meta.IsStatusConditionTrue(cr.Status.Conditions, "Available") {
			info := mlflowInfoFromCR(cr, log)
			return info, nil
		}
	}

	log.Info("MLflow CRD present but no Available MLflow CR found, waiting for service readiness")
	info, err := r.waitForMLflowService(ctx, log)
	if err != nil {
		log.Info("MLflow service did not become ready within timeout, skipping MLflow presets",
			"error", err)
		return &mlflowInfo{}, nil
	}
	return info, nil
}

// mlflowInfoFromCR extracts the in-cluster endpoint and workspace namespace
// from an MLflow CR. The MLflow CRD is cluster-scoped, so cr.Namespace is
// always empty; we derive the namespace from status.address.url instead
// (e.g. "https://mlflow.redhat-ods-applications.svc:8443").
func mlflowInfoFromCR(cr *mlflow.MLflow, log logr.Logger) *mlflowInfo {
	info := &mlflowInfo{available: true}

	if cr.Status.Address != nil && cr.Status.Address.URL != "" {
		parsed, err := url.Parse(cr.Status.Address.URL)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			hostname := parsed.Hostname()
			parts := strings.SplitN(hostname, ".", 3)
			if len(parts) >= 2 {
				info.workspaceNS = parts[1]
			}
			info.tracesURL = fmt.Sprintf("%s://%s/v1/traces", parsed.Scheme, parsed.Host)
			log.Info("Found available MLflow CR",
				"name", cr.Name, "addressURL", cr.Status.Address.URL,
				"tracesURL", info.tracesURL, "workspaceNS", info.workspaceNS)
			return info
		}
		log.Info("MLflow address URL missing scheme or host, falling back",
			"url", cr.Status.Address.URL)
	}

	if cr.Status.URL != "" {
		info.tracesURL = strings.TrimRight(cr.Status.URL, "/") + "/v1/traces"
		log.Info("Found available MLflow CR (using external URL)",
			"name", cr.Name, "url", cr.Status.URL, "tracesURL", info.tracesURL)
	} else {
		log.Info("Found available MLflow CR but no endpoint URL in status",
			"name", cr.Name)
	}
	return info
}

// waitForMLflowService retries with backoff until an MLflow CR becomes Available.
func (r *OtelBootstrapRunnable) waitForMLflowService(ctx context.Context, log logr.Logger) (*mlflowInfo, error) {
	var result *mlflowInfo

	backoff := wait.Backoff{
		Duration: defaultBackoffInitial,
		Factor:   2.0,
		Cap:      defaultBackoffMax,
		Steps:    20,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, defaultBackoffTimeout)
	defer cancel()

	err := wait.ExponentialBackoffWithContext(timeoutCtx, backoff, func(ctx context.Context) (bool, error) {
		list := &mlflow.MLflowList{}
		if err := r.APIReader.List(ctx, list); err != nil {
			log.V(1).Info("Retrying MLflow CR list", "error", err)
			return false, nil
		}
		for i := range list.Items {
			cr := &list.Items[i]
			if meta.IsStatusConditionTrue(cr.Status.Conditions, "Available") {
				result = mlflowInfoFromCR(cr, log)
				return true, nil
			}
		}
		log.V(1).Info("MLflow CR not yet Available, retrying")
		return false, nil
	})

	if result == nil {
		result = &mlflowInfo{}
	}
	return result, err
}

// mlflowCRDPresent checks if the mlflows.mlflow.opendatahub.io CRD is installed.
func (r *OtelBootstrapRunnable) mlflowCRDPresent(ctx context.Context) (bool, error) {
	if r.MLflowCRDExists != nil {
		return r.MLflowCRDExists(ctx)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(r.Config)
	if err != nil {
		return false, fmt.Errorf("creating discovery client: %w", err)
	}

	resources, err := dc.ServerResourcesForGroupVersion(mlflowCRDGroup + "/" + mlflowCRDVersion)
	if err != nil {
		return false, nil //nolint:nilerr // CRD not installed is not an error
	}
	for _, res := range resources.APIResources {
		if res.Name == mlflowCRDResource {
			return true, nil
		}
	}
	return false, nil
}

// discoverPhoenix checks if the Phoenix service exists in the operator namespace.
func (r *OtelBootstrapRunnable) discoverPhoenix(ctx context.Context, log logr.Logger) bool {
	svc := &corev1.Service{}
	key := types.NamespacedName{Name: phoenixServiceName, Namespace: r.Namespace}
	if err := r.APIReader.Get(ctx, key, svc); err != nil {
		log.V(1).Info("Phoenix service not found, skipping Phoenix preset",
			"namespace", r.Namespace)
		return false
	}
	log.Info("Phoenix service detected", "namespace", r.Namespace)
	return true
}

// rolloutRestartCollector patches the OTel collector Deployment's pod template
// annotation to trigger a rollout restart.
func (r *OtelBootstrapRunnable) rolloutRestartCollector(ctx context.Context, log logr.Logger, configHash string) error {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Name: collectorDeployment, Namespace: r.Namespace}
	if err := r.APIReader.Get(ctx, key, dep); err != nil {
		if errors.IsNotFound(err) {
			log.Info("OTel collector Deployment not found, skipping rollout restart")
			return nil
		}
		return fmt.Errorf("reading OTel collector Deployment: %w", err)
	}

	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = make(map[string]string)
	}

	if dep.Spec.Template.Annotations[restartAnnotation] == configHash {
		log.Info("OTel collector Deployment already has current config hash, skipping restart")
		return nil
	}

	dep.Spec.Template.Annotations[restartAnnotation] = configHash
	if err := r.Client.Update(ctx, dep); err != nil {
		return fmt.Errorf("rollout-restarting OTel collector: %w", err)
	}
	log.Info("Triggered OTel collector rollout restart", "configHash", configHash)
	return nil
}

// assembleCollectorConfig builds the complete OTel collector YAML config by
// merging the base config with component-specific presets.
func assembleCollectorConfig(isOCP bool, mf *mlflowInfo, phoenixAvailable bool) (map[string]any, error) {
	config, err := parsePreset(baseConfig)
	if err != nil {
		return nil, fmt.Errorf("parsing base config: %w", err)
	}

	hasComponentPipeline := false

	if phoenixAvailable {
		phoenix, err := parsePreset(phoenixPreset)
		if err != nil {
			return nil, fmt.Errorf("parsing phoenix preset: %w", err)
		}
		mergeDeep(config, phoenix)
		hasComponentPipeline = true
	}

	if mf.available {
		mlflowCfg, err := parsePreset(mlflowPreset)
		if err != nil {
			return nil, fmt.Errorf("parsing mlflow preset: %w", err)
		}
		mergeDeep(config, mlflowCfg)
		hasComponentPipeline = true

		if mf.tracesURL != "" {
			setMLflowTracesEndpoint(config, mf.tracesURL)
		}

		if isOCP {
			rhoaiAuth, err := parsePreset(rhoaiMlflowAuthPreset)
			if err != nil {
				return nil, fmt.Errorf("parsing rhoai mlflow auth preset: %w", err)
			}
			mergeDeep(config, rhoaiAuth)

			clearMLflowExporterTLS(config)
			setMLflowBearerTokenAuth(config, mf.workspaceNS)
		} else {
			mlflowAuth, err := parsePreset(mlflowAuthPreset)
			if err != nil {
				return nil, fmt.Errorf("parsing mlflow auth preset: %w", err)
			}
			mergeDeep(config, mlflowAuth)

			setMLflowOAuthAuth(config)
		}
	}

	if !hasComponentPipeline {
		defaultCfg, err := parsePreset(defaultPreset)
		if err != nil {
			return nil, fmt.Errorf("parsing default preset: %w", err)
		}
		mergeDeep(config, defaultCfg)
	}

	if isOCP && mf.available {
		setIngressCATLS(config)
	}

	return config, nil
}

// setMLflowTracesEndpoint overrides the hardcoded preset endpoint with the
// dynamically discovered in-cluster URL from the MLflow CR.
func setMLflowTracesEndpoint(config map[string]any, tracesURL string) {
	exporters, ok := config["exporters"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp, ok := exporters["otlphttp/mlflow"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp["traces_endpoint"] = tracesURL
}

// parsePreset unmarshals a YAML string into a map.
func parsePreset(yamlStr string) (map[string]any, error) {
	var result map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// mergeDeep recursively merges src into dst. Maps are merged recursively;
// all other values (including slices) in src overwrite dst.
func mergeDeep(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal
			continue
		}

		dstMap, dstOk := dstVal.(map[string]any)
		srcMap, srcOk := srcVal.(map[string]any)
		if dstOk && srcOk {
			mergeDeep(dstMap, srcMap)
		} else {
			dst[key] = srcVal
		}
	}
}

// clearMLflowExporterTLS clears the TLS config on the MLflow exporter for RHOAI
// (RHOAI uses service-ca.crt from the SA token projection).
func clearMLflowExporterTLS(config map[string]any) {
	exporters, ok := config["exporters"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp, ok := exporters["otlphttp/mlflow"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp["tls"] = map[string]any{}
}

// setMLflowBearerTokenAuth sets bearer token auth and workspace headers on the
// MLflow exporter for RHOAI deployments.
func setMLflowBearerTokenAuth(config map[string]any, mlflowNamespace string) {
	exporters, ok := config["exporters"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp, ok := exporters["otlphttp/mlflow"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp["auth"] = map[string]any{"authenticator": "bearertokenauth/mlflow"}

	headers, ok := mlflowExp["headers"].(map[string]any)
	if !ok {
		headers = map[string]any{}
		mlflowExp["headers"] = headers
	}
	headers["x-mlflow-workspace"] = mlflowNamespace
}

// setMLflowOAuthAuth sets OAuth2 client authentication on the MLflow exporter
// for self-managed MLflow deployments.
func setMLflowOAuthAuth(config map[string]any) {
	exporters, ok := config["exporters"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp, ok := exporters["otlphttp/mlflow"].(map[string]any)
	if !ok {
		return
	}
	mlflowExp["auth"] = map[string]any{"authenticator": "oauth2client/mlflow"}
}

// setIngressCATLS sets the ingress CA file path on the OAuth2 client extension
// for OpenShift deployments.
func setIngressCATLS(config map[string]any) {
	extensions, ok := config["extensions"].(map[string]any)
	if !ok {
		return
	}
	oauth, ok := extensions["oauth2client/mlflow"].(map[string]any)
	if !ok {
		return
	}
	oauth["tls"] = map[string]any{"ca_file": "/etc/pki/ingress-ca/ingress-ca.pem"}
}

// componentSummary returns a human-readable summary of detected components.
func componentSummary(mlflow, phoenix bool) string {
	s := ""
	if mlflow {
		s += "mlflow "
	}
	if phoenix {
		s += "phoenix "
	}
	if s == "" {
		s = "default (no component pipelines)"
	}
	return s
}

// parseGroupVersion extracts the group from a "group/version" string.
func parseGroupVersion(gv string) (string, error) {
	for i := len(gv) - 1; i >= 0; i-- {
		if gv[i] == '/' {
			return gv[:i], nil
		}
	}
	return "", fmt.Errorf("no slash in group/version %q", gv)
}
