package controllers

import (
	dioscuriv1 "github.com/amazeeio/dioscuri/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	// LabelAppName for discovery.
	LabelAppName = "dioscuri.amazee.io/service-name"
	// LabelAppType for discovery.
	LabelAppType = "dioscuri.amazee.io/type"
	// LabelAppManaged for discovery.
	LabelAppManaged = "dioscuri.amazee.io/managed-by"
)

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

// RouteContainsStatus check if conditions contains a condition
func RouteContainsStatus(slice []dioscuriv1.RouteMigrateConditions, s dioscuriv1.RouteMigrateConditions) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// IngressContainsStatus check if conditions contains a condition
func IngressContainsStatus(slice []dioscuriv1.IngressMigrateConditions, s dioscuriv1.IngressMigrateConditions) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// HostMigrationContainsStatus check if conditions contains a condition
func HostMigrationContainsStatus(slice []dioscuriv1.HostMigrationConditions, s dioscuriv1.HostMigrationConditions) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
