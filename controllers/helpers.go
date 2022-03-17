package controllers

import (
	"encoding/json"

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

// ProcessAnnotationRules will return new annotations based on a given ruleset and existing annotations
func ProcessAnnotationRules(ruleset string, annotations *map[string]interface{}) (map[string]interface{}, error) {
	var rules Rules
	if err := json.Unmarshal([]byte(ruleset), &rules); err != nil {
		return nil, err
	}
	newAnnos := *annotations
	for _, rule := range rules {
		ifOk := 0
		for _, rif := range rule.If {
			switch rif.Operator {
			case "doesnotexist":
				dne := true
				for key := range *annotations {
					if key == rif.Name {
						dne = false
					}
				}
				if dne {
					ifOk++
				}
			case "equals":
				for key, val := range *annotations {
					if rif.Name == key && rif.Value == val {
						ifOk++
					}
				}
			}
		}
		if ifOk == len(rule.If) {
			for _, rthen := range rule.Then {
				switch rthen.Operator {
				case "source":
					updateAnnotation(rthen.Name, &newAnnos, getAnnotationValue(rthen.Value, *annotations))
				}
			}
			for _, rthen := range rule.Then {
				switch rthen.Operator {
				case "equals":
					updateAnnotation(rthen.Name, &newAnnos, rthen.Value)
				}
			}
			for _, rthen := range rule.Then {
				switch rthen.Operator {
				case "delete":
					removeAnnotation(rthen.Name, &newAnnos)
				}
			}
			break
		}
	}
	return newAnnos, nil
}

func removeAnnotation(annotation string, annotations *map[string]interface{}) {
	// delete(*annotations, annotation)
	(*annotations)[annotation] = nil
}

func updateAnnotation(annotation string, annotations *map[string]interface{}, value string) {
	(*annotations)[annotation] = value
}

func getAnnotationValue(annotation string, annotations map[string]interface{}) string {
	return annotations[annotation].(string)
}

// Rules .
type Rules []struct {
	If   []RulesIf   `json:"if"`
	Then []RulesThen `json:"then"`
}

// RulesIf .
type RulesIf struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Operator string `json:"operator"`
}

// RulesThen .
type RulesThen struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Operator string `json:"operator"`
}
