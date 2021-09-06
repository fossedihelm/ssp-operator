package template_validator

import (
	"fmt"

	templatev1 "github.com/openshift/api/template/v1"
	admission "k8s.io/api/admissionregistration/v1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	kubevirt "kubevirt.io/client-go/api/v1"

	"kubevirt.io/ssp-operator/internal/common"
	common_templates "kubevirt.io/ssp-operator/internal/operands/common-templates"
	webhook "kubevirt.io/ssp-operator/internal/template-validator/webhooks"
)

const (
	ContainerPort          = 8443
	MetricsPort            = 8443
	KubevirtIo             = "kubevirt.io"
	SecretName             = "virt-template-validator-certs"
	VirtTemplateValidator  = "virt-template-validator"
	ClusterRoleName        = "template:view"
	ClusterRoleBindingName = "template-validator"
	WebhookName            = VirtTemplateValidator
	ServiceAccountName     = "template-validator"
	ServiceName            = VirtTemplateValidator
	DeploymentName         = VirtTemplateValidator
	PrometheusLabel        = "prometheus.kubevirt.io"
)

func commonLabels() map[string]string {
	return map[string]string{
		KubevirtIo: VirtTemplateValidator,
	}
}

func getTemplateValidatorImage() string {
	return common.EnvOrDefault(common.TemplateValidatorImageKey, defaultTemplateValidatorImage)
}

func newClusterRole() *rbac.ClusterRole {
	return &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClusterRoleName,
			Namespace: "",
			Labels: map[string]string{
				KubevirtIo: "",
			},
		},
		Rules: []rbac.PolicyRule{{
			APIGroups: []string{templatev1.GroupName},
			Resources: []string{"templates"},
			Verbs:     []string{"get", "list", "watch"},
		}, {
			APIGroups: []string{kubevirt.GroupName},
			Resources: []string{"virtualmachines"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}
}

func newServiceAccount(namespace string) *core.ServiceAccount {
	return &core.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName,
			Namespace: namespace,
			Labels:    commonLabels(),
		},
	}
}

func newClusterRoleBinding(namespace string) *rbac.ClusterRoleBinding {
	return &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClusterRoleBindingName,
			Namespace: "",
			Labels:    commonLabels(),
		},
		RoleRef: rbac.RoleRef{
			Kind:     "ClusterRole",
			Name:     ClusterRoleName,
			APIGroup: rbac.GroupName,
		},
		Subjects: []rbac.Subject{{
			Kind:      "ServiceAccount",
			Name:      ServiceAccountName,
			Namespace: namespace,
		}},
	}
}

func newService(namespace string) *core.Service {
	return &core.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName,
			Namespace: namespace,
			Labels:    commonLabels(),
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": SecretName,
			},
		},
		Spec: core.ServiceSpec{
			Ports: []core.ServicePort{{
				Name:       "webhook",
				Port:       443,
				TargetPort: intstr.FromInt(ContainerPort),
			}},
			Selector: commonLabels(),
		},
	}
}

func newDeployment(namespace string, replicas int32, image string) *apps.Deployment {
	const volumeName = "tls"
	const certMountPath = "/etc/webhook/certs"
	trueVal := true

	podLabels := commonLabels()
	podLabels[PrometheusLabel] = ""
	podLabels["name"] = DeploymentName

	return &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName,
			Namespace: namespace,
			Labels: map[string]string{
				"name": DeploymentName,
			},
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: commonLabels(),
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   VirtTemplateValidator,
					Labels: podLabels,
				},
				Spec: core.PodSpec{
					ServiceAccountName: ServiceAccountName,
					PriorityClassName:  "system-cluster-critical",
					Containers: []core.Container{{
						Name:            "webhook",
						Image:           image,
						ImagePullPolicy: core.PullAlways,
						Resources: core.ResourceRequirements{
							Requests: core.ResourceList{
								core.ResourceCPU:    resource.MustParse("50m"),
								core.ResourceMemory: resource.MustParse("150Mi"),
							},
						},
						Args: []string{
							"-v=2",
							fmt.Sprintf("--port=%d", ContainerPort),
							fmt.Sprintf("--cert-dir=%s", certMountPath),
						},
						VolumeMounts: []core.VolumeMount{{
							Name:      volumeName,
							MountPath: certMountPath,
							ReadOnly:  true,
						}},
						SecurityContext: &core.SecurityContext{
							ReadOnlyRootFilesystem: &trueVal,
						},
						Ports: []core.ContainerPort{{
							Name:          "webhook",
							ContainerPort: ContainerPort,
							Protocol:      core.ProtocolTCP,
						}, {
							Name:          "metrics",
							ContainerPort: MetricsPort,
							Protocol:      core.ProtocolTCP,
						}},
						ReadinessProbe: &core.Probe{
							Handler: core.Handler{
								HTTPGet: &core.HTTPGetAction{
									Path:   "/readyz",
									Port:   intstr.FromInt(ContainerPort),
									Scheme: core.URISchemeHTTPS,
								},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       10,
						},
					}},
					Volumes: []core.Volume{{
						Name: volumeName,
						VolumeSource: core.VolumeSource{
							Secret: &core.SecretVolumeSource{
								SecretName: SecretName,
							},
						},
					}},
				},
			},
		},
	}
}

func newValidatingWebhook(serviceNamespace string) *admission.ValidatingWebhookConfiguration {
	fail := admission.Fail
	sideEffectsNone := admission.SideEffectClassNone

	var vmRules []admission.RuleWithOperations
	for _, version := range kubevirt.ApiSupportedWebhookVersions {
		vmRules = append(vmRules, admission.RuleWithOperations{
			Operations: []admission.OperationType{
				admission.Create, admission.Update,
			},
			Rule: admission.Rule{
				APIGroups:   []string{kubevirt.GroupName},
				APIVersions: []string{version},
				Resources:   []string{"virtualmachines"},
			},
		})
	}

	return &admission.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: WebhookName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admission.ValidatingWebhook{{
			Name: "virtualmachine-admission.ssp.kubevirt.io",
			ClientConfig: admission.WebhookClientConfig{
				Service: &admission.ServiceReference{
					Name:      ServiceName,
					Namespace: serviceNamespace,
					Path:      pointer.StringPtr(webhook.VmValidatePath),
				},
			},
			Rules:                   vmRules,
			FailurePolicy:           &fail,
			SideEffects:             &sideEffectsNone,
			AdmissionReviewVersions: []string{"v1"},
		}, {
			Name: "template-admission.ssp.kubevirt.io",
			ClientConfig: admission.WebhookClientConfig{
				Service: &admission.ServiceReference{
					Name:      ServiceName,
					Namespace: serviceNamespace,
					Path:      pointer.StringPtr(webhook.TemplateValidatePath),
				},
			},
			ObjectSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					common_templates.TemplateTypeLabel: common_templates.TemplateTypeLabelBaseValue,
				},
			},
			Rules: []admission.RuleWithOperations{{
				Operations: []admission.OperationType{
					admission.Delete,
				},
				Rule: admission.Rule{
					APIGroups:   []string{templatev1.GroupVersion.Group},
					APIVersions: []string{templatev1.GroupVersion.Version},
					Resources:   []string{"templates"},
				},
			}},
			FailurePolicy:           &fail,
			SideEffects:             &sideEffectsNone,
			AdmissionReviewVersions: []string{"v1"},
		}},
	}
}
