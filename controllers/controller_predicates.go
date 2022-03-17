package controllers

// contains all the event watch conditions for secret and ingresses

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// IngressPredicates .
type IngressPredicates struct {
	predicate.Funcs
}

// Create .
func (IngressPredicates) Create(e event.CreateEvent) bool {
	// handle "dioscuri.amazee.io/migrate" annotation
	if _, ok := e.Meta.GetAnnotations()["dioscuri.amazee.io/migrate"]; ok {
		return true
	}
	return false
}

// Delete .
func (IngressPredicates) Delete(e event.DeleteEvent) bool {
	// handle "dioscuri.amazee.io/migrate" annotation
	if _, ok := e.Meta.GetAnnotations()["dioscuri.amazee.io/migrate"]; ok {
		return true
	}
	return false
}

// Update .
func (IngressPredicates) Update(e event.UpdateEvent) bool {
	// handle "dioscuri.amazee.io/migrate" annotation
	if _, ok := e.MetaNew.GetAnnotations()["dioscuri.amazee.io/migrate"]; ok {
		if _, okOld := e.MetaOld.GetAnnotations()["dioscuri.amazee.io/migrate"]; okOld {
			return true
		}
	}
	return false
}

// Generic .
func (IngressPredicates) Generic(e event.GenericEvent) bool {
	// handle "dioscuri.amazee.io/migrate" annotation
	if _, ok := e.Meta.GetAnnotations()["dioscuri.amazee.io/migrate"]; ok {
		return true
	}
	return false
}
