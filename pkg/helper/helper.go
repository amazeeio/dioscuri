package helper

// Helper functions to check and remove string from a slice of strings.

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// ContainsStatus check if conditions contains a condition
func ContainsStatus(slice []interface{}, s interface{}) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
