package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kaimeraaiv1 "github.com/kaimera-ai/kaimera/api/v1"
)

// ModelDeploymentReconciler reconciles a ModelDeployment object
type ModelDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kaimera.ai,resources=modeldeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kaimera.ai,resources=modeldeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kaimera.ai,resources=modeldeployments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ModelDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ModelDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("in reconcile")

	md := kaimeraaiv1.ModelDeployment{}
	err := r.Get(ctx, req.NamespacedName, &md)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("in reconcile got model deployment with model", "model", md.Spec.ModelName)

	dp := appsv1.Deployment{}
	err = r.Get(ctx, req.NamespacedName, &dp)
	logger.Info("in reconcile got deployment", "deployment", dp.Name)
	if err != nil {

		// Create new deployment
		logger.Info("creating deployment")
		// Insert func
		deploy, err := r.generateDeployment(&md)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.Create(ctx, deploy)
		if err != nil {
			return ctrl.Result{}, err
		}

		svc, err := r.generateService(&md)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.Create(ctx, svc)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Update an existing deployment
		deploy, err := r.generateDeployment(&md)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.Update(ctx, deploy)
		if err != nil {
			return ctrl.Result{}, err
		}

		svc, err := r.generateService(&md)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.Update(ctx, svc)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kaimeraaiv1.ModelDeployment{}).
		Complete(r)
}

func (r *ModelDeploymentReconciler) generateDeployment(md *kaimeraaiv1.ModelDeployment) (*appsv1.Deployment, error) {

	if md.Spec.Replicas == 0 {
		md.Spec.Replicas = 1
	}

	var image string
	maxModelLength := 512
	if md.Spec.MaxModelLength > 0 {
		maxModelLength = int(md.Spec.MaxModelLength)
	}
	var tolerations []corev1.Toleration
	var limits corev1.ResourceList
	if md.Spec.Runtime == "" || md.Spec.Runtime == "cpu" {
		image = "patnaikshekhar/vllm-cpu:1"
	} else if md.Spec.Runtime == "gpu" {
		image = "vllm/vllm-openai:latest"
		tolerations = []corev1.Toleration{
			{
				Key:      "nvidia.com/gpu",
				Operator: "Exists",
				Effect:   "NoSchedule",
			},
		}

		limits = corev1.ResourceList{
			"nvidia.com/gpu": resource.MustParse("1"),
		}

	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      md.Name,
			Namespace: md.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &md.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": md.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": md.Name,
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector: md.Spec.NodeSelectorLabels,
					Containers: []corev1.Container{
						{
							Name:            "app",
							Image:           image,
							ImagePullPolicy: "IfNotPresent",
							Command: []string{
								"vllm",
								"serve",
								"--dtype",
								"auto",
								"--max-model-len",
								fmt.Sprintf("%d", maxModelLength),
								md.Spec.ModelName,
							},
							Resources: corev1.ResourceRequirements{
								Limits: limits,
							},
						},
					},
					Tolerations: tolerations,
				},
			},
		},
	}

	err := ctrl.SetControllerReference(md, deploy, r.Scheme)
	if err != nil {
		return nil, err
	}

	return deploy, nil
}

func (r *ModelDeploymentReconciler) generateService(md *kaimeraaiv1.ModelDeployment) (*corev1.Service, error) {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      md.Name,
			Namespace: md.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": md.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(8000),
					Port:       80,
				},
			},
		},
	}, nil
}
