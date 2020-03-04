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
	"fmt"

	dioscuriv1 "github.com/amazeeio/dioscuri/api/v1"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RouteMigrateReconciler reconciles a RouteMigrate object
type RouteMigrateReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Labels map[string]string
}

const (
	// LabelAppName for discovery.
	LabelAppName = "dioscuri.amazee.io/service-name"
	// LabelAppType for discovery.
	LabelAppType = "dioscuri.amazee.io/type"
	// LabelAppManaged for discovery.
	LabelAppManaged = "dioscuri.amazee.io/managed-by"
)

// +kubebuilder:rbac:groups=dioscuri.amazee.io,resources=routemigrates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dioscuri.amazee.io,resources=routemigrates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete

func (r *RouteMigrateReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	opLog := r.Log.WithValues("routemigrate", req.NamespacedName)
	// your logic here
	var dioscuri dioscuriv1.RouteMigrate
	if err := r.Get(ctx, req.NamespacedName, &dioscuri); err != nil {
		return ctrl.Result{}, IgnoreNotFound(err)
	}
	// your logic here
	finalizerName := "finalizer.dioscuri.amazee.io/v1"

	labels := map[string]string{
		LabelAppName:    dioscuri.ObjectMeta.Name,
		LabelAppType:    "dioscuri",
		LabelAppManaged: "route-migration",
	}
	r.Labels = labels

	// examine DeletionTimestamp to determine if object is under deletion
	if dioscuri.ObjectMeta.DeletionTimestamp.IsZero() {
		// check if the migrate annotation is set to true
		if dioscuri.Annotations["dioscuri.amazee.io/migrate"] == "true" {
			// first set the migration to false after we start
			// this way we shouldn't proceed to do any more changes if there is an error as there might be something wrong further down.
			dioscuri.Annotations["dioscuri.amazee.io/migrate"] = "false"
			if err := r.Update(context.Background(), &dioscuri); err != nil {
				return ctrl.Result{}, err
			}
			sourceNamespace := dioscuri.ObjectMeta.Namespace
			destinationNamespace := dioscuri.Spec.DestinationNamespace
			opLog.Info(fmt.Sprintf("Beginning route migration checks for routes in %s moving to %s", sourceNamespace, destinationNamespace))
			// START ACME-CHALLENGE CLEANUP SECTION
			// check routes in the source namespace for any pending acme-challenges
			// check if any routes in the source namespace have an exposer label, we need to remove these before we move any routes
			acmeLabels := map[string]string{"acme.openshift.io/exposer": "true"}
			acmeSourceToDestination := &routev1.RouteList{}
			if err := r.getRoutesWithLabel(&dioscuri, acmeSourceToDestination, sourceNamespace, acmeLabels); err != nil {
				opLog.Info(fmt.Sprintf("%v", err))
			}
			if err := r.cleanUpAcmeChallenges(&dioscuri, acmeSourceToDestination); err != nil {
				return ctrl.Result{}, err
			}
			acmeDestinationToSource := &routev1.RouteList{}
			if err := r.getRoutesWithLabel(&dioscuri, acmeDestinationToSource, destinationNamespace, acmeLabels); err != nil {
				opLog.Info(fmt.Sprintf("%v", err))
			}
			if err := r.cleanUpAcmeChallenges(&dioscuri, acmeDestinationToSource); err != nil {
				return ctrl.Result{}, err
			}
			// END ACME-CHALLENGE CLEANUP SECTION
			// START CHECKING SERVICES SECTION
			migrateLabels := map[string]string{"dioscuri.amazee.io/migrate": "true"}
			// get the routes from the source namespace, these will get moved to the destination namespace
			routesSourceToDestination := &routev1.RouteList{}
			if err := r.getRoutesWithLabel(&dioscuri, routesSourceToDestination, sourceNamespace, migrateLabels); err != nil {
				opLog.Info(fmt.Sprintf("%v", err))
			}
			// get the routes from the destination namespace, these will get moved to the source namespace
			routesDestinationToSource := &routev1.RouteList{}
			if err := r.getRoutesWithLabel(&dioscuri, routesDestinationToSource, destinationNamespace, migrateLabels); err != nil {
				opLog.Info(fmt.Sprintf("%v", err))
			}
			// check that the services for the routes we are moving exist in each namespace
			migrateSourceToDestination := &routev1.RouteList{}
			r.checkServices(&dioscuri, routesSourceToDestination, migrateSourceToDestination, destinationNamespace)
			migrateDestinationToSource := &routev1.RouteList{}
			r.checkServices(&dioscuri, routesDestinationToSource, migrateDestinationToSource, destinationNamespace)
			// END CHECKING SERVICES SECTION
			// START MIGRATING ROUTES SECTION
			// actually start the migrations here
			for _, route := range migrateSourceToDestination.Items {
				// migrate these routes
				if result, err := r.individualRouteMigration(&dioscuri, &route, sourceNamespace, destinationNamespace); err != nil {
					return result, err
				}
			}
			for _, route := range migrateDestinationToSource.Items {
				// migrate these routes
				if result, err := r.individualRouteMigration(&dioscuri, &route, destinationNamespace, sourceNamespace); err != nil {
					return result, err
				}
			}
		}
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !ContainsString(dioscuri.ObjectMeta.Finalizers, finalizerName) {
			dioscuri.ObjectMeta.Finalizers = append(dioscuri.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(context.Background(), &dioscuri); err != nil {
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
			if err := r.Update(context.Background(), &dioscuri); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *RouteMigrateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dioscuriv1.RouteMigrate{}).
		Complete(r)
}

func (r *RouteMigrateReconciler) deleteExternalResources(dioscuri *dioscuriv1.RouteMigrate, namespace string) error {
	// delete any external resources associated with the autoidler
	return nil
}

func (r *RouteMigrateReconciler) checkServices(dioscuri *dioscuriv1.RouteMigrate, routeList *routev1.RouteList, routesToMigrate *routev1.RouteList, destinationNamespace string) {
	// check service for route exists in destination namespace
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	for _, route := range routeList.Items {
		service := &corev1.Service{}
		err := r.Get(context.TODO(), types.NamespacedName{Namespace: destinationNamespace, Name: route.Spec.To.Name}, service)
		if err != nil {
			opLog.Info(fmt.Sprintf("Service %s for route %s doesn't exist in namespace %s, skipping route", route.Spec.To.Name, route.ObjectMeta.Name, destinationNamespace))
		} else {
			routesToMigrate.Items = append(routesToMigrate.Items, route)
		}
	}
	// return nil
}

func (r *RouteMigrateReconciler) getRoutesWithLabel(dioscuri *dioscuriv1.RouteMigrate, routes *routev1.RouteList, namespace string, labels map[string]string) error {
	// collect any routes with specific labels
	listOption := (&client.ListOptions{}).ApplyOptions([]client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	})
	if err := r.List(context.TODO(), routes, listOption); err != nil {
		return fmt.Errorf("Unable to get any routes: %v", err)
	}
	return nil
}

func (r *RouteMigrateReconciler) cleanUpAcmeChallenges(dioscuri *dioscuriv1.RouteMigrate, routeList *routev1.RouteList) error {
	// we need to ensure there are no stale or pending acme challenges for any routes we are going to
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	for _, route := range routeList.Items {
		opLog.Info(fmt.Sprintf("Found acme-challenge for %s, proceeding to delete the pending challenge before moving the route", route.Spec.Host))
		// deep copy the route
		acmeRoute := &routev1.Route{}
		route.DeepCopyInto(acmeRoute)
		if err := r.removeRoute(acmeRoute); err != nil {
			// we should break here before we try to migrate the routes, broken acme is better than broken site
			return fmt.Errorf("Unable to acme-challenge route %s in %s: %v", acmeRoute.ObjectMeta.Name, acmeRoute.ObjectMeta.Namespace, err)
		}
	}
	return nil
}

func (r *RouteMigrateReconciler) individualRouteMigration(dioscuri *dioscuriv1.RouteMigrate, route *routev1.Route, sourceNamespace string, destinationNamespace string) (ctrl.Result, error) {
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	oldRoute := &routev1.Route{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: sourceNamespace, Name: route.ObjectMeta.Name}, oldRoute)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("Route %s in namespace %s doesn't exist: %v", route.ObjectMeta.Name, sourceNamespace, err)
	}
	// if we ever need to do anything for any routes with `tls-acme: true` enabled on them, for now, info only
	// if oldRoute.Annotations["kubernetes.io/tls-acme"] == "true" {
	// 	opLog.Info(fmt.Sprintf("Lets Encrypt is enabled for %s", oldRoute.Spec.Host))
	// }
	// actually migrate here
	// we need to create a new route now, but we need to swap the namespace to the destination.
	// deepcopyinto from old to the new route
	newRoute := &routev1.Route{}
	oldRoute.DeepCopyInto(newRoute)
	// set the newroute namespace as the destination namespace
	newRoute.ObjectMeta.Namespace = destinationNamespace
	newRoute.ObjectMeta.ResourceVersion = ""
	opLog.Info(fmt.Sprintf("Attempting to migrate route %s - %s", newRoute.ObjectMeta.Name, newRoute.Spec.Host))
	if err := r.migrateRoute(dioscuri, newRoute, oldRoute, r.Labels); err != nil {
		return ctrl.Result{}, fmt.Errorf("Error migrating route %s in namespace %s: %v", route.ObjectMeta.Name, sourceNamespace, err)
	}
	opLog.Info(fmt.Sprintf("Done migrating route %s", route.ObjectMeta.Name))
	return ctrl.Result{}, nil
}

// add routes, and then remove the old one only if we successfully create the new one
func (r *RouteMigrateReconciler) migrateRoute(dioscuri *dioscuriv1.RouteMigrate, newRoute *routev1.Route, oldRoute *routev1.Route, labels map[string]string) error {
	// add route
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	if err := r.addRouteIfNotExist(dioscuri, newRoute); err != nil {
		return fmt.Errorf("Unable to create route %s in %s: %v", newRoute.ObjectMeta.Name, newRoute.ObjectMeta.Namespace, err)
	}
	// delete old route from the old namespace
	opLog.Info(fmt.Sprintf("Removing old route %s in namespace %s", oldRoute.ObjectMeta.Name, oldRoute.ObjectMeta.Namespace))
	if err := r.removeRoute(oldRoute); err != nil {
		return fmt.Errorf("Unable to remove old route %s in %s: %v", oldRoute.ObjectMeta.Name, oldRoute.ObjectMeta.Namespace, err)
	}
	return nil
}

// add any routes if they don't already exist in the new namespace
func (r *RouteMigrateReconciler) addRouteIfNotExist(dioscuri *dioscuriv1.RouteMigrate, route *routev1.Route) error {
	// add route
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	opLog.Info(fmt.Sprintf("Getting existing route %s in namespace %s", route.ObjectMeta.Name, route.ObjectMeta.Namespace))
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: route.ObjectMeta.Namespace, Name: route.ObjectMeta.Name}, route)
	if err != nil {
		// there is no route in the destination namespace, then we create it
		opLog.Info(fmt.Sprintf("Creating route %s in namespace %s", route.ObjectMeta.Name, route.ObjectMeta.Namespace))
		if err := r.Create(context.Background(), route); err != nil {
			return fmt.Errorf("Unable to create route %s in %s: %v", route.ObjectMeta.Name, route.ObjectMeta.Namespace, err)
		}
	}
	return nil
}

// remove a given route
func (r *RouteMigrateReconciler) removeRoute(route *routev1.Route) error {
	// remove route
	if err := r.Delete(context.Background(), route); err != nil {
		return fmt.Errorf("Unable to delete route %s in %s: %v", route.ObjectMeta.Name, route.ObjectMeta.Namespace, err)
	}
	return nil
}

// IgnoreNotFound will ignore not found errors
func IgnoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// ContainsString check if a slice contains a string
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// RemoveString remove string from a sliced
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

// ContainsStatus check if conditions contains a condition
func ContainsStatus(slice []interface{}, s interface{}) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
