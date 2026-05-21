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

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
)

const (
	// ClusterDefaultsConfigMapName is the ConfigMap containing platform-wide webhook defaults.
	ClusterDefaultsConfigMapName = "kagenti-platform-config"

	// ClusterFeatureGatesConfigMapName is the ConfigMap containing feature gate settings.
	ClusterFeatureGatesConfigMapName = "kagenti-feature-gates"

	// ClusterDefaultsNamespace is the namespace where cluster-level ConfigMaps live.
	ClusterDefaultsNamespace = "kagenti-system"

	// LabelNamespaceDefaults identifies namespace-level defaults ConfigMaps.
	LabelNamespaceDefaults = "kagenti.io/defaults"

	// AuthBridgeRuntimeConfigMapName is the namespace-scoped ConfigMap that
	// holds the authbridge runtime config (config.yaml). Edits to this
	// ConfigMap are watched by AgentRuntimeReconciler so the resolved-config
	// hash picks them up and rolls affected workloads.
	AuthBridgeRuntimeConfigMapName = "authbridge-runtime-config"
)

// resolvedConfig is the canonical representation used for hash computation.
// It captures the merged result of cluster defaults → namespace defaults → CR overrides.
//
// Structured fields (Type, TrustDomain, Trace) hold CR-level overrides.
// FeatureGates and Defaults hold the raw ConfigMap data. The hash is computed
// from the full struct — the webhook performs the same merge independently
// at Pod CREATE time.
type resolvedConfig struct {
	Type         string            `json:"type"`
	TrustDomain  string            `json:"trustDomain,omitempty"`
	Trace        *traceConfig      `json:"trace,omitempty"`
	FeatureGates map[string]string `json:"featureGates,omitempty"`
	Defaults     map[string]string `json:"defaults,omitempty"`

	// AuthBridgeMode and MTLSMode change the injected sidecar shape /
	// transport posture, both of which require a pod restart to take
	// effect. Including them here folds CR-edit changes into the
	// config-hash so applyWorkloadConfig stamps a new hash on the pod
	// template and the Deployment rolls.
	AuthBridgeMode string `json:"authBridgeMode,omitempty"`
	MTLSMode       string `json:"mtlsMode,omitempty"`

	// AuthBridgeRuntime captures the namespace authbridge-runtime-config
	// ConfigMap's config.yaml content so namespace-level edits flow into
	// the hash. Stored as the raw string (not parsed) because authbridge
	// pipelines/listener/mtls config drift through here in any shape and
	// we want any byte change to roll the workload. Empty string when
	// the ConfigMap doesn't exist in the namespace.
	AuthBridgeRuntime string `json:"authBridgeRuntime,omitempty"`
}

type traceConfig struct {
	Endpoint string  `json:"endpoint,omitempty"`
	Protocol string  `json:"protocol,omitempty"`
	Rate     float64 `json:"rate,omitempty"`
}

// ConfigResult holds the computed hash and any warnings from the config resolution.
type ConfigResult struct {
	Hash     string
	Warnings []string
}

// ComputeConfigHash computes a deterministic SHA256 hash from the 3-layer
// merged configuration: cluster defaults → namespace defaults → AgentRuntime CR.
// Both the controller and webhook perform the same merge independently.
func ComputeConfigHash(ctx context.Context, c client.Reader, namespace string, spec *agentv1alpha1.AgentRuntimeSpec) (ConfigResult, error) {
	resolved, warnings := resolveConfig(ctx, c, namespace, spec)
	hash, err := hashResolvedConfig(resolved)
	if err != nil {
		return ConfigResult{}, err
	}
	return ConfigResult{Hash: hash, Warnings: warnings}, nil
}

// ComputeDefaultsOnlyHash computes a hash using only cluster + namespace defaults
// (no CR overrides). Used when an AgentRuntime is deleted to trigger a rolling
// update back to platform defaults.
func ComputeDefaultsOnlyHash(ctx context.Context, c client.Reader, namespace string) (string, error) {
	resolved, _ := resolveConfig(ctx, c, namespace, nil)
	return hashResolvedConfig(resolved)
}

// resolveConfig merges the three configuration layers:
// 1. Cluster defaults (ConfigMaps in kagenti-system)
// 2. Namespace defaults (ConfigMap with kagenti.io/defaults=true label)
// 3. AgentRuntime CR spec (highest priority)
func resolveConfig(ctx context.Context, c client.Reader, namespace string, spec *agentv1alpha1.AgentRuntimeSpec) (resolvedConfig, []string) {
	logger := log.FromContext(ctx)
	var warnings []string

	// Layer 1: cluster defaults
	clusterDefaults := readConfigMapData(ctx, c, ClusterDefaultsNamespace, ClusterDefaultsConfigMapName)
	featureGates := readConfigMapData(ctx, c, ClusterDefaultsNamespace, ClusterFeatureGatesConfigMapName)

	// Layer 2: namespace defaults (override cluster)
	nsDefaults, nsWarning := readNamespaceDefaults(ctx, c, namespace)
	if nsWarning != "" {
		warnings = append(warnings, nsWarning)
	}
	merged := mergeMaps(clusterDefaults, nsDefaults)

	// Layer 2b: namespace authbridge-runtime-config (config.yaml).
	// Captured raw so any byte change rolls the workload. The CM may
	// not exist in every agent namespace; absence is normal and the
	// admission webhook falls back to its own defaults.
	abRuntime := ""
	if data := readConfigMapData(ctx, c, namespace, AuthBridgeRuntimeConfigMapName); len(data) > 0 {
		abRuntime = data["config.yaml"]
	}

	resolved := resolvedConfig{
		FeatureGates:      featureGates,
		Defaults:          merged,
		AuthBridgeRuntime: abRuntime,
	}

	if spec == nil {
		logger.V(2).Info("Resolved config with defaults only", "namespace", namespace)
		return resolved, warnings
	}

	// Layer 3: CR overrides (highest priority).
	// Structured fields capture only CR-level overrides so they don't
	// duplicate values already present in the Defaults map.
	resolved.Type = string(spec.Type)

	if spec.Identity != nil && spec.Identity.SPIFFE != nil && spec.Identity.SPIFFE.TrustDomain != "" {
		resolved.TrustDomain = spec.Identity.SPIFFE.TrustDomain
	}

	if spec.Trace != nil {
		resolved.Trace = &traceConfig{}
		if spec.Trace.Endpoint != "" {
			resolved.Trace.Endpoint = spec.Trace.Endpoint
		}
		if spec.Trace.Protocol != "" {
			resolved.Trace.Protocol = string(spec.Trace.Protocol)
		}
		if spec.Trace.Sampling != nil {
			resolved.Trace.Rate = spec.Trace.Sampling.Rate
		}
	}

	resolved.AuthBridgeMode = spec.AuthBridgeMode
	resolved.MTLSMode = spec.MTLSMode

	return resolved, warnings
}

// readConfigMapData reads a specific ConfigMap by name and namespace.
// Returns an empty map if the ConfigMap does not exist.
func readConfigMapData(ctx context.Context, c client.Reader, namespace, name string) map[string]string {
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm); err != nil {
		log.FromContext(ctx).V(2).Info("ConfigMap not found, using empty defaults",
			"namespace", namespace, "name", name, "error", err)
		return map[string]string{}
	}
	if cm.Data == nil {
		return map[string]string{}
	}
	return cm.Data
}

// readNamespaceDefaults reads the namespace-level defaults ConfigMap.
// Expects at most one ConfigMap with the kagenti.io/defaults=true label per namespace.
// Returns the ConfigMap data and a warning if multiple ConfigMaps are found.
func readNamespaceDefaults(ctx context.Context, c client.Reader, namespace string) (map[string]string, string) {
	logger := log.FromContext(ctx)

	cmList := &corev1.ConfigMapList{}
	if err := c.List(ctx, cmList,
		client.InNamespace(namespace),
		client.MatchingLabels{LabelNamespaceDefaults: "true"},
	); err != nil {
		logger.V(2).Info("Failed to list namespace defaults ConfigMaps", "namespace", namespace, "error", err)
		return map[string]string{}, ""
	}

	if len(cmList.Items) == 0 {
		return map[string]string{}, ""
	}

	var warning string
	if len(cmList.Items) > 1 {
		names := make([]string, len(cmList.Items))
		for i := range cmList.Items {
			names[i] = cmList.Items[i].Name
		}
		warning = fmt.Sprintf(
			"multiple namespace defaults ConfigMaps found in %s (expected at most one): %v; using %s",
			namespace, names, cmList.Items[0].Name,
		)
		logger.Error(fmt.Errorf("%s", warning), "Ambiguous namespace defaults")
	}

	if cmList.Items[0].Data == nil {
		return map[string]string{}, warning
	}
	return cmList.Items[0].Data, warning
}

// mergeMaps merges two maps. Values in override take precedence over base.
func mergeMaps(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

// hashResolvedConfig produces a deterministic SHA256 hex string from the resolved config.
// encoding/json sorts map keys, ensuring deterministic output.
func hashResolvedConfig(resolved resolvedConfig) (string, error) {
	b, err := json.Marshal(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to marshal resolved config: %w", err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
