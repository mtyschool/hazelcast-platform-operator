package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	hazelcastv1alpha1 "github.com/hazelcast/hazelcast-enterprise-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	finalizer      = "hazelcast.com/finalizer"
	licenseDataKey = "license-key"
)

func (r *HazelcastReconciler) addFinalizer(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	if !controllerutil.ContainsFinalizer(h, finalizer) {
		controllerutil.AddFinalizer(h, finalizer)
		err := r.Update(ctx, h)
		if err != nil {
			logger.Error(err, "Failed to add finalizer into custom resource")
			return err
		}
		logger.V(1).Info("Finalizer added into custom resource successfully")
	}
	return nil
}

func (r *HazelcastReconciler) executeFinalizer(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	if err := r.removeClusterRole(ctx, h, logger); err != nil {
		logger.Error(err, "ClusterRole could not be removed")
		return err
	}
	controllerutil.RemoveFinalizer(h, finalizer)
	err := r.Update(ctx, h)
	if err != nil {
		logger.Error(err, "Failed to remove finalizer from custom resource")
		return err
	}
	return nil
}

func (r *HazelcastReconciler) reconcileClusterRole(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: objectMetadataForHazelcast(h),
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, clusterRole, func() error {
		clusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"endpoints", "pods", "nodes", "services"},
				Verbs:     []string{"get", "list"},
			},
		}
		return nil
	})
	if opResult != controllerutil.OperationResultNone {
		logger.Info("Operation result", "ClusterRole", h.Name, "result", opResult)
	}
	return err
}

func (r *HazelcastReconciler) reconcileServiceAccount(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: objectNamespacedMetadataForHazelcast(h),
	}

	err := controllerutil.SetControllerReference(h, serviceAccount, r.Scheme)
	if err != nil {
		logger.Error(err, "Failed to set owner reference on ServiceAccount")
		return err
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, serviceAccount, func() error {
		return nil
	})
	if opResult != controllerutil.OperationResultNone {
		logger.Info("Operation result", "ServiceAccount", h.Name, "result", opResult)
	}
	return err
}

func (r *HazelcastReconciler) reconcileRoleBinding(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: objectNamespacedMetadataForHazelcast(h),
	}

	err := controllerutil.SetControllerReference(h, roleBinding, r.Scheme)
	if err != nil {
		return err
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, roleBinding, func() error {
		roleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      h.Name,
				Namespace: h.Namespace,
			},
		}
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     h.Name,
		}

		return nil
	})
	if opResult != controllerutil.OperationResultNone {
		logger.Info("Operation result", "RoleBinding", h.Name, "result", opResult)
	}
	return err
}

func (r *HazelcastReconciler) reconcileService(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	service := &corev1.Service{
		ObjectMeta: objectNamespacedMetadataForHazelcast(h),
	}

	err := controllerutil.SetControllerReference(h, service, r.Scheme)
	if err != nil {
		logger.Error(err, "Failed to set owner reference on Service")
		return err
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec = corev1.ServiceSpec{
			Type:      v1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labelsForHazelcast(h),
			Ports: []corev1.ServicePort{
				{
					Name:       "hazelcast-port",
					Protocol:   corev1.ProtocolTCP,
					Port:       5701,
					TargetPort: intstr.FromString("hazelcast"),
				},
			},
		}
		return nil
	})
	if opResult != controllerutil.OperationResultNone {
		logger.Info("Operation result", "Service", h.Name, "result", opResult)
	}
	return err
}

// reconcileStatefulset deploys the StatefulSet of Hazelcast resource.
// The returned boolean returns true if the StatefulSet is ready and false otherwise.
func (r *HazelcastReconciler) reconcileStatefulset(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	sts := &appsv1.StatefulSet{
		ObjectMeta: objectNamespacedMetadataForHazelcast(h),
	}

	err := controllerutil.SetControllerReference(h, sts, r.Scheme)
	if err != nil {
		logger.Error(err, "Failed to set owner reference on Statefulset")
		return err
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
		replicas := h.Spec.ClusterSize
		ls := labelsForHazelcast(h)
		sts.Spec = appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			ServiceName: h.Name,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: v1.PodSpec{
					ServiceAccountName: h.Name,
					Containers: []v1.Container{{
						Image: imageForCluster(h),
						Name:  "hazelcast",
						Ports: []v1.ContainerPort{{
							ContainerPort: 5701,
							Name:          "hazelcast",
						}},
						Env: []v1.EnvVar{
							{
								Name: "HZ_LICENSEKEY",
								ValueFrom: &v1.EnvVarSource{
									SecretKeyRef: &v1.SecretKeySelector{
										LocalObjectReference: v1.LocalObjectReference{
											Name: h.Spec.LicenseKeySecret,
										},
										Key: licenseDataKey,
									},
								},
							},
							{
								Name:  "HZ_NETWORK_JOIN_KUBERNETES_ENABLED",
								Value: "true",
							},
							{
								Name:  "HZ_NETWORK_JOIN_KUBERNETES_SERVICENAME",
								Value: h.Name,
							},
							{
								Name:  "HZ_NETWORK_RESTAPI_ENABLED",
								Value: "true",
							},
							{
								Name:  "HZ_NETWORK_RESTAPI_ENDPOINTGROUPS_HEALTHCHECK_ENABLED",
								Value: "true",
							},
						},
						LivenessProbe: &v1.Probe{
							Handler: v1.Handler{
								HTTPGet: &v1.HTTPGetAction{
									Path:   "/hazelcast/health/node-state",
									Port:   intstr.FromInt(5701),
									Scheme: "HTTP",
								},
							},
							InitialDelaySeconds: 0,
							TimeoutSeconds:      10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							FailureThreshold:    10,
						},
						ReadinessProbe: &v1.Probe{
							Handler: v1.Handler{
								HTTPGet: &v1.HTTPGetAction{
									Path:   "/hazelcast/health/node-state",
									Port:   intstr.FromInt(5701),
									Scheme: "HTTP",
								},
							},
							InitialDelaySeconds: 0,
							TimeoutSeconds:      10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							FailureThreshold:    10,
						},
						SecurityContext: &v1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{65534}[0],
							Privileged:               &[]bool{false}[0],
							ReadOnlyRootFilesystem:   &[]bool{true}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &v1.Capabilities{
								Drop: []v1.Capability{"ALL"},
							},
						},
					}},
					TerminationGracePeriodSeconds: &[]int64{600}[0],
				},
			},
		}
		return nil
	})
	if opResult != controllerutil.OperationResultNone {
		logger.Info("Operation result", "Statefulset", h.Name, "result", opResult)
	}
	return err
}

func (r *HazelcastReconciler) checkIfRunning(ctx context.Context, h *hazelcastv1alpha1.Hazelcast) bool {
	sts := &appsv1.StatefulSet{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: h.Name, Namespace: h.Namespace}, sts)
	if err != nil {
		return false
	}
	return isStatefulSetReady(sts, h.Spec.ClusterSize)
}

func isStatefulSetReady(sts *appsv1.StatefulSet, expectedReplicas int32) bool {
	allUpdated := expectedReplicas == sts.Status.UpdatedReplicas
	allReady := expectedReplicas == sts.Status.ReadyReplicas
	atExpectedGeneration := sts.Generation == sts.Status.ObservedGeneration
	return allUpdated && allReady && atExpectedGeneration
}

func (r *HazelcastReconciler) removeClusterRole(ctx context.Context, h *hazelcastv1alpha1.Hazelcast, logger logr.Logger) error {
	clusterRole := &rbacv1.ClusterRole{}
	err := r.Get(ctx, client.ObjectKey{Name: h.Name}, clusterRole)
	if err != nil && errors.IsNotFound(err) {
		logger.V(1).Info("ClusterRole is not created yet. Or it is already removed.")
		return nil
	}

	err = r.Delete(ctx, clusterRole)
	if err != nil {
		logger.Error(err, "Failed to clean up ClusterRole")
		return err
	}
	logger.V(1).Info("ClusterRole removed successfully")
	return nil
}

func labelsForHazelcast(h *hazelcastv1alpha1.Hazelcast) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "hazelcast",
		"app.kubernetes.io/instance":   h.Name,
		"app.kubernetes.io/managed-by": "hazelcast-enterprise-operator",
	}
}

func objectNamespacedMetadataForHazelcast(h *hazelcastv1alpha1.Hazelcast) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      h.Name,
		Namespace: h.Namespace,
		Labels:    labelsForHazelcast(h),
	}
}

func objectMetadataForHazelcast(h *hazelcastv1alpha1.Hazelcast) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:   h.Name,
		Labels: labelsForHazelcast(h),
	}
}

func imageForCluster(h *hazelcastv1alpha1.Hazelcast) string {
	return fmt.Sprintf("%s:%s", h.Spec.Repository, h.Spec.Version)
}
