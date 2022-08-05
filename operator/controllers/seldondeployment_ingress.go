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
)

var (
	KubernetesIngressEnabled   = utils.GetEnvAsBool("KUBERNETES_INGRESS_ENABLED", false)
	KubernetesIngressClassName = utils.GetEnv("KUBERNETES_INGRESS_CLASS_NAME", "")
	KubernetesIngressHost      = utils.GetEnv("KUBERNETES_INGRESS_HOST", "")
)

func (r *IngressCreator) CreateIngress(ctx context.Context, instance *machinelearningv1.SeldonDeployment) (bool, error) {
	ready := true

	if ok, err := r.createKubernetesIngress(ctx, instance); err != nil {
		return false, err
	} else if !ok {
		ready = false
	}

	return ready, nil
}

func (r IngressCreator) createKubernetesIngress(ctx context.Context, instance *machinelearningv1.SeldonDeployment) (bool, error) {
	if !KubernetesIngressEnabled {
		return true, nil
	}
	ready := false

	log := logr.FromContextOrDiscard(ctx)
	log.Info("Creating Kubernetes Ingress")

	ingress := &networkingv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	updatefun := func() error {
		ingress.Annotations = instance.Spec.Annotations
		ingress.Spec = networkingv1.IngressSpec{
			IngressClassName: func() *string {
				if classname := getAnnotation(instance, ANNOTATION_INGRESS_CLASS_NAME, KubernetesIngressClassName); classname != "" {
					return &classname
				}
				return nil
			}(),
			Rules: []networkingv1.IngressRule{
				{
					Host: getAnnotation(instance, ANNOTATION_INGRESS_HOST, KubernetesIngressHost),
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: func() []networkingv1.HTTPIngressPath {
								paths := make([]networkingv1.HTTPIngressPath, 0, len(instance.Spec.Predictors))
								for _, p := range instance.Spec.Predictors {
									pSvcName := machinelearningv1.GetPredictorKey(instance, &p)
									paths = append(paths, networkingv1.HTTPIngressPath{
										Path: r.ingressPathPrefix(instance),
										PathType: func() *networkingv1.PathType {
											pt := networkingv1.PathTypePrefix
											return &pt
										}(),
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: pSvcName,
												Port: networkingv1.ServiceBackendPort{
													Name: constants.HttpPortName,
												},
											},
										},
									})
								}
								return paths
							}(),
						},
					},
				},
			},
		}
		return nil
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, updatefun); err != nil {
		return ready, nil
	}
	ready = true
	return ready, nil
}

type IngressCreator struct {
	Client client.Client
}

func (r *IngressCreator) ingressPathPrefix(instance *machinelearningv1.SeldonDeployment) string {
	return "/seldon/" + instance.Namespace + "/" + instance.Name + "/"
}
