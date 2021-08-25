/*

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
	"encoding/json"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dioscuriv1 "github.com/amazeeio/dioscuri/api/v1"
)

// HostMigrationReconciler reconciles a HostMigration object
type HostMigrationReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Labels    map[string]string
	Openshift bool
}

// +kubebuilder:rbac:groups=dioscuri.amazee.io,resources=hostmigrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dioscuri.amazee.io,resources=hostmigrations/status,verbs=get;update;patch

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create

// +kubebuilder:rbac:groups="*",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="*",resources=ingress/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="*",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="*",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="*",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="cert-manager.io",resources=certificates,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the actual reconcilation process
func (r *HostMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//	ctx := context.Background()
	opLog := r.Log.WithValues("hostmigration", req.NamespacedName)

	// your logic here
	var dioscuri dioscuriv1.HostMigration
	if err := r.Get(ctx, req.NamespacedName, &dioscuri); err != nil {
		return ctrl.Result{}, IgnoreNotFound(err)
	}

	finalizerName := "finalizer.dioscuri.amazee.io/v1"

	labels := map[string]string{
		LabelAppName:    dioscuri.ObjectMeta.Name,
		LabelAppType:    "dioscuri",
		LabelAppManaged: "host-migration",
	}
	r.Labels = labels

	// examine DeletionTimestamp to determine if object is under deletion
	if dioscuri.ObjectMeta.DeletionTimestamp.IsZero() {
		// check if the migrate annotation is set to true
		if dioscuri.Annotations["dioscuri.amazee.io/migrate"] == "true" {
			if r.Openshift {
				// if this controller is running in openshift
				// run the openshift handler side of the process
				r.OpenshiftHandler(ctx, opLog, dioscuri)
			} else {
				// otherwise its probably kubernetes
				r.KubernetesHandler(ctx, opLog, dioscuri)
			}
		}
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !ContainsString(dioscuri.ObjectMeta.Finalizers, finalizerName) {
			dioscuri.ObjectMeta.Finalizers = append(dioscuri.ObjectMeta.Finalizers, finalizerName)
			mergePatch, _ := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"finalizers": dioscuri.ObjectMeta.Finalizers,
				},
			})
			if err := r.Patch(ctx, &dioscuri, client.RawPatch(types.MergePatchType, mergePatch)); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if ContainsString(dioscuri.ObjectMeta.Finalizers, finalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(&dioscuri, req.NamespacedName.Namespace); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}
			// remove our finalizer from the list and update it.
			dioscuri.ObjectMeta.Finalizers = RemoveString(dioscuri.ObjectMeta.Finalizers, finalizerName)
			mergePatch, _ := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"finalizers": dioscuri.ObjectMeta.Finalizers,
				},
			})
			if err := r.Patch(ctx, &dioscuri, client.RawPatch(types.MergePatchType, mergePatch)); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager setups the controller with a manager
func (r *HostMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dioscuriv1.HostMigration{}).
		Complete(r)
}
