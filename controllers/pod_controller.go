/*
Copyright 2021.

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

package controllers

import (
	"context"
	"fmt"
	"regexp"

	dcrenixiov1alpha1 "gitlab.enix.io/products/docker-cache-registry/api/v1alpha1"
	"gitlab.enix.io/products/docker-cache-registry/internal/cache"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.
		FromContext(ctx).
		WithValues("pod", req.NamespacedName)

	log.Info("reconciling pod")
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cachedImages, err := r.desiredCachedImages(pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("cachedImages", "quantity", len(cachedImages))

	applyOpts := []client.PatchOption{
		client.FieldOwner("pod-controller"),
		client.ForceOwnership,
	}

	for _, cachedImage := range cachedImages {
		_ = cachedImage
		err = r.Patch(ctx, &cachedImage, client.Apply, applyOpts...)
		if err != nil {
			log.Error(err, "couldn't patch cachedimage", "cachedImage", klog.KObj(&cachedImage))
			return ctrl.Result{}, err
		}
		log.Info("cachedimage patched", "cachedImage", klog.KObj(&cachedImage))
	}

	log.Info("reconciled pod")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

func (r *PodReconciler) desiredCachedImages(pod corev1.Pod) ([]dcrenixiov1alpha1.CachedImage, error) {
	containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
	cachedImages := []dcrenixiov1alpha1.CachedImage{}

	for i, container := range containers {
		sourceImage, ok := pod.Annotations[fmt.Sprintf("tugger-original-image-%d", i)]
		if !ok {
			// klog.V(2).InfoS("missing source image, ignoring", "pod", klog.KObj(pod), "container", container.Name)
			continue
		}
		re := regexp.MustCompile(`localhost:[0-9]+/`)
		image := string(re.ReplaceAll([]byte(container.Image), []byte("")))
		sanitizedName := cache.SanitizeImageName(image)
		cachedImage := dcrenixiov1alpha1.CachedImage{
			TypeMeta: metav1.TypeMeta{APIVersion: dcrenixiov1alpha1.GroupVersion.String(), Kind: "CachedImage"},
			ObjectMeta: metav1.ObjectMeta{
				Name: sanitizedName,
			},
			Spec: dcrenixiov1alpha1.CachedImageSpec{
				Image:       image,
				SourceImage: sourceImage,
			},
		}

		// "You have to implement your own ownership and garbage collector implementation." - https://github.com/operator-framework/operator-sdk/issues/1914#issuecomment-532257704

		// if err := ctrl.SetControllerReference(&pod, &cachedImage, r.Scheme); err != nil {
		// 	return cachedImages, err
		// }

		cachedImages = append(cachedImages, cachedImage)
	}

	return cachedImages, nil
}