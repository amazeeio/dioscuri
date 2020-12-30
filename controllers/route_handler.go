package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dioscuriv1 "github.com/amazeeio/dioscuri/api/v1"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OpenshiftHandler handles doing route migrations in an openshift cluster
func (r *HostMigrationReconciler) OpenshiftHandler(ctx context.Context, opLog logr.Logger, dioscuri dioscuriv1.HostMigration) (ctrl.Result, error) {
	var activeMigratedRoutes []string
	var standbyMigratedRoutes []string
	// first set the migration to false after we start
	// this way we shouldn't proceed to do any more changes if there is an error as there might be something wrong further down.
	mergePatch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				"dioscuri.amazee.io/migrate": "false",
			},
		},
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("Unable to create mergepatch for %s, error was: %v", dioscuri.ObjectMeta.Name, err)
	}
	if err := r.Patch(ctx, &dioscuri, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
		return ctrl.Result{}, fmt.Errorf("Unable to patch routemigrate %s, error was: %v", dioscuri.ObjectMeta.Name, err)
	}
	r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
		Status:    "True",
		Type:      "started",
		Condition: "Started route migration",
	}, activeMigratedRoutes, standbyMigratedRoutes)
	sourceNamespace := dioscuri.ObjectMeta.Namespace
	destinationNamespace := dioscuri.Spec.DestinationNamespace
	opLog.Info(fmt.Sprintf("Beginning route migration checks for routes in %s moving to %s", sourceNamespace, destinationNamespace))

	// check destination namespace exists
	namespace := corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: destinationNamespace}, &namespace); err != nil {
		opLog.Info(fmt.Sprintf("Unable to find destination namespace, error was: %v", err))
		return ctrl.Result{}, nil
	}

	// START ACME-CHALLENGE CLEANUP SECTION
	// check routes in the source namespace for any pending acme-challenges
	// check if any routes in the source namespace have an exposer label, we need to remove these before we move any routes
	acmeLabels := map[string]string{"acme.openshift.io/exposer": "true"}
	acmeSourceToDestination := &routev1.RouteList{}
	if err := r.getRoutesWithLabel(&dioscuri, acmeSourceToDestination, sourceNamespace, acmeLabels); err != nil {
		opLog.Info(fmt.Sprintf("%v", err))
	}
	if err := r.cleanUpAcmeChallenges(&dioscuri, acmeSourceToDestination); err != nil {
		r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
			Status:    "True",
			Type:      "failed",
			Condition: fmt.Sprintf("%v", err),
		}, activeMigratedRoutes, standbyMigratedRoutes)
		return ctrl.Result{}, err
	}
	acmeDestinationToSource := &routev1.RouteList{}
	if err := r.getRoutesWithLabel(&dioscuri, acmeDestinationToSource, destinationNamespace, acmeLabels); err != nil {
		opLog.Info(fmt.Sprintf("%v", err))
	}
	if err := r.cleanUpAcmeChallenges(&dioscuri, acmeDestinationToSource); err != nil {
		r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
			Status:    "True",
			Type:      "failed",
			Condition: fmt.Sprintf("%v", err),
		}, activeMigratedRoutes, standbyMigratedRoutes)
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
	r.checkServices(&dioscuri, routesDestinationToSource, migrateDestinationToSource, sourceNamespace)
	// END CHECKING SERVICES SECTION
	// START MIGRATING ROUTES SECTION
	// actually start the migrations here
	var migratedRoutes []MigratedRoutes
	for _, route := range migrateSourceToDestination.Items {
		// migrate these routes
		newRoute, err := r.individualRouteMigration(&dioscuri, &route, sourceNamespace, destinationNamespace)
		if err != nil {
			r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
				Status:    "True",
				Type:      "failed",
				Condition: fmt.Sprintf("%v", err),
			}, activeMigratedRoutes, standbyMigratedRoutes)
			return ctrl.Result{}, err
		}
		migratedRoutes = append(migratedRoutes, MigratedRoutes{NewRoute: newRoute, OldRouteNamespace: sourceNamespace})
		routeScheme := "http://"
		if route.Spec.TLS != nil {
			if route.Spec.TLS.Termination != "" {
				routeScheme = "https://"
			}
		}
		standbyMigratedRoutes = append(standbyMigratedRoutes, fmt.Sprintf("%s%s", routeScheme, route.Spec.Host))
	}
	for _, route := range migrateDestinationToSource.Items {
		// migrate these routes
		newRoute, err := r.individualRouteMigration(&dioscuri, &route, destinationNamespace, sourceNamespace)
		if err != nil {
			r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
				Status:    "True",
				Type:      "failed",
				Condition: fmt.Sprintf("%v", err),
			}, activeMigratedRoutes, standbyMigratedRoutes)
			return ctrl.Result{}, err
		}
		// add the migrated route so we go through and fix them up later
		migratedRoutes = append(migratedRoutes, MigratedRoutes{NewRoute: newRoute, OldRouteNamespace: destinationNamespace})
		routeScheme := "http://"
		if route.Spec.TLS != nil {
			if route.Spec.TLS.Termination != "" {
				routeScheme = "https://"
			}
		}
		activeMigratedRoutes = append(activeMigratedRoutes, fmt.Sprintf("%s%s", routeScheme, route.Spec.Host))
	}
	// wait a sec before updating the routes
	checkInterval := time.Duration(1)
	time.Sleep(checkInterval * time.Second)
	// once we move all the routes, we have to go through and do a final update on them to make sure any `HostAlreadyClaimed` warning/errors go away
	for _, migratedRoute := range migratedRoutes {
		err := r.updateRoute(&dioscuri, migratedRoute.NewRoute, migratedRoute.OldRouteNamespace)
		if err != nil {
			r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
				Status:    "True",
				Type:      "failed",
				Condition: fmt.Sprintf("%v", err),
			}, activeMigratedRoutes, standbyMigratedRoutes)
			return ctrl.Result{}, err
		}
	}
	r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.HostMigrationConditions{
		Status:    "True",
		Type:      "completed",
		Condition: "Completed route migration",
	}, activeMigratedRoutes, standbyMigratedRoutes)
	return ctrl.Result{}, nil
}

func (r *HostMigrationReconciler) deleteExternalResources(dioscuri *dioscuriv1.HostMigration, namespace string) error {
	// delete any external resources associated with the autoidler
	return nil
}

func (r *HostMigrationReconciler) checkServices(dioscuri *dioscuriv1.HostMigration,
	routeList *routev1.RouteList,
	routesToMigrate *routev1.RouteList,
	destinationNamespace string,
) {
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

func (r *HostMigrationReconciler) getRoutesWithLabel(dioscuri *dioscuriv1.HostMigration,
	routes *routev1.RouteList,
	namespace string,
	labels map[string]string,
) error {
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

func (r *HostMigrationReconciler) cleanUpAcmeChallenges(dioscuri *dioscuriv1.HostMigration, routeList *routev1.RouteList) error {
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

func (r *HostMigrationReconciler) individualRouteMigration(dioscuri *dioscuriv1.HostMigration,
	route *routev1.Route,
	sourceNamespace string,
	destinationNamespace string,
) (*routev1.Route, error) {
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	oldRoute := &routev1.Route{}
	newRoute := &routev1.Route{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: sourceNamespace, Name: route.ObjectMeta.Name}, oldRoute)
	if err != nil {
		return newRoute, fmt.Errorf("Route %s in namespace %s doesn't exist: %v", route.ObjectMeta.Name, sourceNamespace, err)
	}
	// if we ever need to do anything for any routes with `tls-acme: true` enabled on them, for now, info only
	// if oldRoute.Annotations["kubernetes.io/tls-acme"] == "true" {
	// 	opLog.Info(fmt.Sprintf("Lets Encrypt is enabled for %s", oldRoute.Spec.Host))
	// }
	// actually migrate here
	// we need to create a new route now, but we need to swap the namespace to the destination.
	// deepcopyinto from old to the new route
	oldRoute.DeepCopyInto(newRoute)
	// set the newroute namespace as the destination namespace
	newRoute.ObjectMeta.Namespace = destinationNamespace
	newRoute.ObjectMeta.ResourceVersion = ""
	opLog.Info(fmt.Sprintf("Attempting to migrate route %s - %s", newRoute.ObjectMeta.Name, newRoute.Spec.Host))
	if err := r.migrateRoute(dioscuri, newRoute, oldRoute); err != nil {
		return newRoute, fmt.Errorf("Error migrating route %s in namespace %s: %v", route.ObjectMeta.Name, sourceNamespace, err)
	}
	opLog.Info(fmt.Sprintf("Done migrating route %s", route.ObjectMeta.Name))
	return newRoute, nil
}

// add routes, and then remove the old one only if we successfully create the new one
func (r *HostMigrationReconciler) migrateRoute(dioscuri *dioscuriv1.HostMigration, newRoute *routev1.Route, oldRoute *routev1.Route) error {
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

func (r *HostMigrationReconciler) updateRoute(dioscuri *dioscuriv1.HostMigration, newRoute *routev1.Route, oldRouteNamespace string) error {
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	// check a few times to make sure the old route no longer exists
	for i := 0; i < 10; i++ {
		oldRouteExists := r.checkOldRouteExists(dioscuri, newRoute, oldRouteNamespace)
		if !oldRouteExists {
			// the new route with label to ensure the router picks it up after we do the deletion
			mergePatch, err := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"dioscuri.amazee.io/migrated-from": oldRouteNamespace,
					},
				},
			})
			if err != nil {
				return fmt.Errorf("Unable to create mergepatch for %s, error was: %v", newRoute.ObjectMeta.Name, err)
			}
			opLog.Info(fmt.Sprintf("Patching route %s in namespace %s", newRoute.ObjectMeta.Name, newRoute.ObjectMeta.Namespace))
			if err := r.Patch(context.Background(), newRoute, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
				return fmt.Errorf("Unable to patch route %s, error was: %v", newRoute.ObjectMeta.Name, err)
			}
			return nil
		}
		// wait 5 secs before re-trying
		checkInterval := time.Duration(5)
		time.Sleep(checkInterval * time.Second)
	}
	return fmt.Errorf("There was an error checking if the old route still exists before trying to patch the new route, there may be an issue with the routes")
}

// add any routes if they don't already exist in the new namespace
func (r *HostMigrationReconciler) addRouteIfNotExist(dioscuri *dioscuriv1.HostMigration, route *routev1.Route) error {
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

func (r *HostMigrationReconciler) checkOldRouteExists(dioscuri *dioscuriv1.HostMigration, route *routev1.Route, sourceNamespace string) bool {
	opLog := r.Log.WithValues("routemigrate", dioscuri.ObjectMeta.Namespace)
	opLog.Info(fmt.Sprintf("Checking route %s is not in source namespace %s", route.ObjectMeta.Name, sourceNamespace))
	getRoute := &routev1.Route{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: sourceNamespace, Name: route.ObjectMeta.Name}, getRoute)
	if err != nil {
		// there is no route in the source namespace
		opLog.Info(fmt.Sprintf("Route %s is not in source namespace %s", route.ObjectMeta.Name, sourceNamespace))
		return false
	}
	opLog.Info(fmt.Sprintf("Route %s is in source namespace %s", route.ObjectMeta.Name, sourceNamespace))
	return true
}

// remove a given route
func (r *HostMigrationReconciler) removeRoute(route *routev1.Route) error {
	// remove route
	if err := r.Delete(context.Background(), route); err != nil {
		return fmt.Errorf("Unable to delete route %s in %s: %v", route.ObjectMeta.Name, route.ObjectMeta.Namespace, err)
	}
	return nil
}

// update status
func (r *HostMigrationReconciler) updateStatusCondition(ctx context.Context,
	dioscuri *dioscuriv1.HostMigration,
	condition dioscuriv1.HostMigrationConditions,
	activeRoutes []string,
	standbyRotues []string,
) error {
	// set the transition time
	condition.LastTransitionTime = time.Now().UTC().Format(time.RFC3339)
	if !HostMigrationContainsStatus(dioscuri.Status.Conditions, condition) {
		dioscuri.Status.Conditions = append(dioscuri.Status.Conditions, condition)
		mergePatch, _ := json.Marshal(map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": dioscuri.Status.Conditions,
			},
			"spec": map[string]interface{}{
				"hosts": map[string]string{
					"activeHosts":  strings.Join(activeRoutes, ","),
					"standbyHosts": strings.Join(standbyRotues, ","),
				},
			},
		})
		if err := r.Patch(ctx, dioscuri, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
			return fmt.Errorf("Unable to update status condition: %v", err)
		}
	}
	return nil
}
