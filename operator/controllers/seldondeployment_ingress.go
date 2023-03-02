package controllers

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	machinelearningv1 "github.com/seldonio/seldon-core/operator/apis/machinelearning.seldon.io/v1"
	"github.com/seldonio/seldon-core/operator/constants"
	utils2 "github.com/seldonio/seldon-core/operator/controllers/utils"
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
	KubernetesIngressPathType  = utils.GetEnv("KUBERNETES_INGRESS_PATH_TYPE", string(networkingv1.PathTypeImplementationSpecific))
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
	ingressBasePath := utils2.GetAnnotation(instance, ANNOTATION_INGRESS_PATH, r.ingressPathPrefix(instance))
	ingressClassName := utils2.GetAnnotation(instance, ANNOTATION_INGRESS_CLASS_NAME, KubernetesIngressClassName)
	ingressPathType := networkingv1.PathType(utils2.GetAnnotation(instance, ANNOTATION_INGRESS_PATH_TYPE, KubernetesIngressPathType))
	ingressHost := utils2.GetAnnotation(instance, ANNOTATION_INGRESS_HOST, KubernetesIngressHost)

	// http ingress
	if err := r.ensureIngress(ctx, instance, ingressClassName, ingressHost, ingressBasePath, &ingressPathType, constants.HttpPortName); err != nil {
		return false, err
	}
	// grpc ingress
	if err := r.ensureIngress(ctx, instance, ingressClassName, ingressHost, "", &ingressPathType, constants.GrpcPortName); err != nil {
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

func (r *IngressCreator) ensureIngress(ctx context.Context,
	instance *machinelearningv1.SeldonDeployment,
	ingressClassName string, ingressHost string,
	httpbasepath string, ingressPathType *networkingv1.PathType,
	portprotocol string,
) error {
	if len(instance.Spec.Predictors) == 0 {
		return nil
	}
	log := logr.FromContextOrDiscard(ctx)
	log.Info("Creating Kubernetes Ingress")

	httprule := &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
		{
			Path: "/",
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: machinelearningv1.GetPredictorKey(instance, &instance.Spec.Predictors[0]),
					Port: networkingv1.ServiceBackendPort{
						Name: portprotocol,
					},
				},
			},
			PathType: ingressPathType,
		},
	}}
	// add a prefied path for http
	if portprotocol == "http" && httpbasepath != "" {
		httprule.Paths[0].Path = httpbasepath
	}
	ingressannotations := map[string]string{}
	for k, v := range instance.Spec.Annotations {
		ingressannotations[k] = v
	}

	// temporary fix for grpc nginx ingress
	if portprotocol == "grpc" {
		delete(ingressannotations, "nginx.ingress.kubernetes.io/rewrite-target")
	}

	ingressname := instance.Name
	if portprotocol != "http" {
		ingressname = instance.Name + "-" + portprotocol
	}
	ingress := &networkingv1.Ingress{ObjectMeta: v1.ObjectMeta{Name: ingressname, Namespace: instance.Namespace}}

	updatefun := func() error {
		if ingress.Annotations == nil {
			ingress.Annotations = make(map[string]string)
		}
		// set backend protocol https://github.com/kubernetes/ingress-nginx/blob/main/docs/user-guide/nginx-configuration/annotations.md#backend-protocol
		ingress.Annotations["nginx.ingress.kubernetes.io/backend-protocol"] = strings.ToUpper(portprotocol)
		for k, v := range ingressannotations {
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
		ingress.Spec.Rules[0].HTTP = httprule
		return controllerutil.SetOwnerReference(instance, ingress, r.Client.Scheme())
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, updatefun); err != nil {
		log.Error(err, "unable create ingress")
		return err
	}
	log.Info("Created Kubernetes Ingress", "name", ingress.Name)
	return nil
}
