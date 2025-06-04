/*
Copyright 2025.

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

package kubernetes

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
	"github.com/kagenti/operator/platform/internal/deployer/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ types.ComponentDeployer = (*KubernetesDeployer)(nil)

type KubernetesDeployer struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

func NewKubernetesDeployer(client client.Client, log logr.Logger, scheme *runtime.Scheme) *KubernetesDeployer {
	log.Info("NewKubernetesDeployer ------ ")
	return &KubernetesDeployer{
		Client: client,
		Log:    log,
		Scheme: scheme,
	}
}
func (b *KubernetesDeployer) GetName() string {
	return "kubernetes"
}
func (d *KubernetesDeployer) Deploy(ctx context.Context, component *platformv1alpha1.Component) error {

	logger := d.Log.WithValues("deployer", component.Name, "Namespace", component.Namespace)
	logger.Info("Deploying component with Kubernetes resources")

	namespace := component.Namespace
	if component.Spec.Deployer.Namespace != "" {
		namespace = component.Spec.Deployer.Namespace
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := d.Client.Create(ctx, ns); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create namespace: %w", err)
		}
	}
	logger.Info("Creating deployment", "component", component.Name, "namespace", component.Namespace)
	if err := d.createDeployment(ctx, component, namespace); err != nil {
		logger.Error(err, "failed to create Deployment")
		return fmt.Errorf("failed to create deployment: %w", err)
	}
	logger.Info("Creating service", "component", component.Name, "namespace", component.Namespace)
	if err := d.createService(ctx, component, namespace); err != nil {
		logger.Error(err, "failed to create Service")
		return fmt.Errorf("failed to create service: %w", err)
	}

	return nil
}

// Update existing component
func (d *KubernetesDeployer) Update(ctx context.Context, component *platformv1alpha1.Component) error {

	return nil
}

// Delete existing component
func (d *KubernetesDeployer) Delete(ctx context.Context, component *platformv1alpha1.Component) error {

	logger := d.Log.WithValues("component", component.Name, "Namespace", component.Namespace)
	logger.Info("Deleting component's Kubernetes resources")

	namespace := component.Namespace
	if component.Spec.Deployer.Namespace != "" {
		namespace = component.Spec.Deployer.Namespace
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      component.Name,
			Namespace: namespace,
		},
	}
	if err := d.Client.Delete(ctx, service); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete service")
			return err
		}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      component.Name,
			Namespace: namespace,
		},
	}
	if err := d.Client.Delete(ctx, deployment); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete deployment")
			return err
		}
	}
	logger.Info("Component's Kubernetes resources deleted succesfully")

	return nil
}

// Return status of the component

func (d *KubernetesDeployer) GetStatus(ctx context.Context, component *platformv1alpha1.Component) (platformv1alpha1.ComponentDeploymentStatus, error) {
	return *component.Status.DeploymentStatus, nil
}

func (d *KubernetesDeployer) createDeployment(ctx context.Context, component *platformv1alpha1.Component, namespace string) error {

	kubeSpec := component.Spec.Deployer.Kubernetes

	if kubeSpec == nil {
		return fmt.Errorf("failed to create deployment - missing expected Spec.Deployer.Kubernetes in the CR")
	}
	containerPorts := []corev1.ContainerPort{}

	if component.Spec.Deployer.Kubernetes.ContainerPorts != nil {
		for _, port := range component.Spec.Deployer.Kubernetes.ContainerPorts {
			containerPorts = append(containerPorts, corev1.ContainerPort{
				Name:          port.Name,
				ContainerPort: port.ContainerPort,
				Protocol:      port.Protocol,
			})
		}
	} else {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          "http",
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolSCTP,
		})

	}
	labels := map[string]string{
		"app.kubernetes.io/name":       component.Name,
		"app.kubernetes.io/part-of":    "platform-operator",
		"app.kuberbetes.io/managed-by": "platform-operator",
		"app.kubernetes.io/component":  getComponentType(component),
	}
	for k, v := range component.Labels {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}
	image := fmt.Sprintf("%s/%s:%s",
		kubeSpec.ImageSpec.ImageRegistry,
		kubeSpec.ImageSpec.Image,
		kubeSpec.ImageSpec.ImageTag,
	)

	deployment := &appsv1.Deployment{

		ObjectMeta: metav1.ObjectMeta{
			Name:      component.Name,
			Namespace: namespace,
			Labels:    labels,
		},

		Spec: appsv1.DeploymentSpec{
			Replicas: getReplicaCount(component),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": component.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            component.Name,
							Image:           image,
							ImagePullPolicy: corev1.PullPolicy(kubeSpec.ImageSpec.ImagePullPolicy),
							Resources:       component.Spec.Deployer.Kubernetes.Resources,
							Env:             component.Spec.Deployer.Env,
							Ports:           containerPorts,
						},
					},
				},
			},
		},
	}

	if component.Annotations != nil {
		deployment.ObjectMeta.Annotations = component.Annotations
	}

	if err := controllerutil.SetControllerReference(component, deployment, d.Client.Scheme()); err != nil {
		return err
	}

	existingDeployment := &appsv1.Deployment{}
	err := d.Client.Get(ctx, k8stypes.NamespacedName{Name: deployment.Name, Namespace: namespace}, existingDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			return d.Client.Create(ctx, deployment)
		}
		return err
	}
	return nil

}
func (d *KubernetesDeployer) createService(ctx context.Context, component *platformv1alpha1.Component, namespace string) error {

	kubeSpec := component.Spec.Deployer.Kubernetes

	if kubeSpec == nil {
		return fmt.Errorf("failed to create service - missing expected Spec.Deployer.Kubernetes in the CR")
	}
	labels := map[string]string{
		"app.kubernetes.io/name":       component.Name,
		"app.kubernetes.io/part-of":    "platform-operator",
		"app.kuberbetes.io/managed-by": "platform-operator",
		"app.kubernetes.io/component":  getComponentType(component),
	}
	for k, v := range component.Labels {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}
	servicePorts := []corev1.ServicePort{}

	if component.Spec.Deployer.Kubernetes.ServicePorts != nil {
		for _, port := range component.Spec.Deployer.Kubernetes.ServicePorts {
			servicePorts = append(servicePorts, corev1.ServicePort{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort,
				Protocol:   corev1.ProtocolTCP,
			})
		}
	} else {
		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       "http",
			Port:       8000,
			TargetPort: intstr.FromInt(8080),
			Protocol:   corev1.ProtocolTCP,
		})

	}
	service := &corev1.Service{

		ObjectMeta: metav1.ObjectMeta{
			Name:      component.Name,
			Namespace: component.Namespace,
			Labels:    labels,
		},

		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name": component.Name,
			},
			Ports: servicePorts,
			Type:  corev1.ServiceType(kubeSpec.ServiceType),
		},
	}
	if component.Annotations != nil {
		service.ObjectMeta.Annotations = component.Annotations
	}

	if err := controllerutil.SetControllerReference(component, service, d.Client.Scheme()); err != nil {
		return err
	}

	existigService := &corev1.Service{}
	err := d.Client.Get(ctx, k8stypes.NamespacedName{Name: service.Name, Namespace: service.Namespace}, existigService)
	if err != nil {
		if errors.IsNotFound(err) {
			d.Log.Info("createService() ---------------------------------- -", "component", component.Name, "Namespace", component.Namespace)
			return d.Client.Create(ctx, service)
		}
		return err
	}
	return nil
}
func getComponentType(component *platformv1alpha1.Component) string {
	if component.Spec.Agent != nil {
		return "agent"
	}
	if component.Spec.Tool != nil {
		return "tool"
	}
	if component.Spec.Infra != nil {
		return "infra"
	}
	return "unknown"
}
func getReplicaCount(component *platformv1alpha1.Component) *int32 {
	var count int32 = 1

	if component.Annotations != nil {
		if replicaStr, ok := component.Annotations["platform.operator.io/replicates"]; ok {
			if replicas, err := strconv.ParseInt(replicaStr, 10, 32); err == nil {
				count = int32(replicas)
			}
		}
	}
	return &count
}
func (d *KubernetesDeployer) CheckComponentStatus(ctx context.Context, component *platformv1alpha1.Component) (bool, string, error) {

	logger := d.Log.WithValues("Kubernetes Deployer", component.Name, "namespace", component.Namespace)
	logger.Info("CheckComponentStatus")

	namespace := component.Namespace
	if component.Spec.Deployer.Namespace != "" {
		namespace = component.Spec.Deployer.Namespace
	}
	deployment := &appsv1.Deployment{}
	err := d.Client.Get(ctx, k8stypes.NamespacedName{Name: component.Name, Namespace: namespace}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, "Deployment not found", nil
		}
		d.Log.Error(err, "Failed to get deployment object")
		return false, fmt.Sprintf("Error checking deployment: %v", err), err
	}
	logger.Info("CheckComponentStatus", "status", deployment.Status)
	if deployment.Status.ReadyReplicas < 1 {
		message := fmt.Sprintf("Deployment not ready: %d/%d replicas available",
			deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
		for _, condition := range deployment.Status.Conditions {
			message = fmt.Sprintf("%s, Reason: %s, Message: %s", message, condition.Reason, condition.Message)
			break
		}
		return false, message, nil
	}
	kubeSpec := component.Spec.Deployer.Kubernetes
	if kubeSpec.ServiceType != "" {
		service := &corev1.Service{}
		err := d.Client.Get(ctx, k8stypes.NamespacedName{Name: component.Name, Namespace: namespace}, service)
		if err != nil {
			if errors.IsNotFound(err) {
				d.Log.Error(err, "!!!!!!!!!!! CheckComponentStatus() -  Service Not Found")
				return false, "Service not found", nil
			}
			d.Log.Error(err, "Failed to get service")
			return false, fmt.Sprintf("Error checking service: %v", err), err
		}
	}
	logger.Info("CheckComponentStatus", "Comoponent is ready", component.Name, "namespace", component.Namespace)
	return true, "Component is ready", nil
}
