package controllers

import (
	"context"

	"github.com/go-logr/logr"
	machinelearningv1 "github.com/seldonio/seldon-core/operator/apis/machinelearning.seldon.io/v1"
	"github.com/seldonio/seldon-core/operator/constants"
	"github.com/seldonio/seldon-core/operator/utils"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ANNOTATION_INGRESS_CLASS_NAME = "seldon.io/ingress-class-name"
	ANNOTATION_INGRESS_HOST       = "seldon.io/ingress-host"
	ANNOTATION_INGRESS_PATH       = "seldon.io/ingress-path"
	ANNOTATION_INGRESS_PATH_TYPE  = "seldon.io/ingress-path-type"
)

var (
	KubernetesIngressEnabled   = utils.GetEnvAsBool("KUBERNETES_INGRESS_ENABLED", false)
	KubernetesIngressClassName = utils.GetEnv("KUBERNETES_INGRESS_CLASS_NAME", "")
	KubernetesIngressHost      = utils.GetEnv("KUBERNETES_INGRESS_HOST", "")
	KubernetesIngressPathType  = utils.GetEnv("KUBERNETES_INGRESS_PATH_TYPE", string(networkingv1.PathTypePrefix))
)

func (r *IngressCreator) CreateIngress(ctx context.Context, instance *machinelearningv1.SeldonDeployment) (bool, error) {
	ready := true

	if k8singressReady, err := r.createKubernetesIngress(ctx, instance); err != nil {
		return false, err
	} else if !k8singressReady {
		ready = false
	}

	return ready, nil
}

func (r IngressCreator) createKubernetesIngress(ctx context.Context, instance *machinelearningv1.SeldonDeployment) (bool, error) {
	if !KubernetesIngressEnabled {
		return true, nil
	}

	log := logr.FromContextOrDiscard(ctx)
	log.Info("Creating Kubernetes Ingress")

	ingress := &networkingv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	ingressBasePath := getAnnotation(instance, ANNOTATION_INGRESS_PATH, r.ingressPathPrefix(instance))
	ingressClassName := getAnnotation(instance, ANNOTATION_INGRESS_CLASS_NAME, KubernetesIngressClassName)
	ingressPathType := networkingv1.PathType(getAnnotation(instance, ANNOTATION_INGRESS_PATH_TYPE, KubernetesIngressPathType))
	ingressHost := getAnnotation(instance, ANNOTATION_INGRESS_HOST, KubernetesIngressHost)

	paths := make([]networkingv1.HTTPIngressPath, 0, len(instance.Spec.Predictors))
	for _, p := range instance.Spec.Predictors {
		path := networkingv1.HTTPIngressPath{
			Path: ingressBasePath,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: machinelearningv1.GetPredictorKey(instance, &p),
					Port: networkingv1.ServiceBackendPort{Name: constants.HttpPortName},
				},
			},
		}
		if ingressPathType != "" {
			path.PathType = &ingressPathType
		}
		paths = append(paths, path)
	}

	updatefun := func() error {
		if ingress.Annotations == nil {
			ingress.Annotations = make(map[string]string)
		}
		for k, v := range instance.Spec.Annotations {
			ingress.Annotations[k] = v
		}
		if ingressClassName != "" {
			ingress.Spec.IngressClassName = &ingressClassName
		}
		if len(ingress.Spec.Rules) == 0 {
			ingress.Spec.Rules = []networkingv1.IngressRule{{}}
		}
		if ingressHost != "" {
			ingress.Spec.Rules[0].Host = ingressHost
		}
		ingress.Spec.Rules[0].HTTP = &networkingv1.HTTPIngressRuleValue{Paths: paths}
		return controllerutil.SetOwnerReference(instance, ingress, r.Client.Scheme())
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, updatefun); err != nil {
		log.Error(err, "unable create ingress")
		return false, err
	}
	return true, nil
}

type IngressCreator struct {
	Client client.Client
}

func (r *IngressCreator) ingressPathPrefix(instance *machinelearningv1.SeldonDeployment) string {
	return "/seldon/" + instance.Namespace + "/" + instance.Name + "/"
}
