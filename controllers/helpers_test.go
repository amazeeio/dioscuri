package controllers

import (
	"reflect"
	"testing"
)

func TestProcessAnnotationRules(t *testing.T) {
	var testCases = map[string]struct {
		input  *map[string]interface{}
		rules  string
		expect map[string]interface{}
	}{
		"pre-migrate": {
			input: &map[string]interface{}{
				"fastly.amazee.io/paused":     "false",
				"fastly.amazee.io/watch":      "true",
				"other.amazee.io/annotation1": "some-other-data",
				"other.amazee.io/annotation2": "more-other-data",
			},
			rules: `[
				{
					"if": [
						{"name": "fastly.amazee.io/paused", "value": "false", "operator": "equals"},
						{"name": "fastly.amazee.io/watch", "value": "true", "operator": "equals"}
					],
					"then": [
						{"name": "fastly.amazee.io/paused", "value": "true", "operator": "equals"},
						{"name": "fastly.amazee.io/watch", "value": "false", "operator": "equals"},
						{"name": "fastly.amazee.io/delete-external-resources", "value": "false", "operator": "equals"},
						{"name": "fastly.amazee.io/backup-paused", "value": "fastly.amazee.io/paused", "operator": "source"},
						{"name": "fastly.amazee.io/backup-watch", "value": "fastly.amazee.io/watch", "operator": "source"}
					]
				}
			]`,
			expect: map[string]interface{}{
				"fastly.amazee.io/paused":                    "true",
				"fastly.amazee.io/watch":                     "false",
				"fastly.amazee.io/backup-paused":             "false",
				"fastly.amazee.io/backup-watch":              "true",
				"fastly.amazee.io/delete-external-resources": "false",
				"other.amazee.io/annotation1":                "some-other-data",
				"other.amazee.io/annotation2":                "more-other-data",
			},
		},
		"post-migrate": {
			input: &map[string]interface{}{
				"fastly.amazee.io/paused":                    "true",
				"fastly.amazee.io/watch":                     "false",
				"fastly.amazee.io/backup-paused":             "false",
				"fastly.amazee.io/backup-watch":              "true",
				"fastly.amazee.io/delete-external-resources": "false",
			},
			rules: `[
				{
					"if": [
					  {"name": "fastly.amazee.io/paused", "value": "true", "operator": "equals"},
					  {"name": "fastly.amazee.io/watch", "value": "false", "operator": "equals"}
					],
					"then": [
					  {"name": "fastly.amazee.io/delete-external-resources", "operator": "delete"},
					  {"name": "fastly.amazee.io/backup-delete-external-resources", "operator": "delete"},
					  {"name": "fastly.amazee.io/backup-paused", "operator": "delete"},
					  {"name": "fastly.amazee.io/backup-watch", "operator": "delete"},
					  {"name": "fastly.amazee.io/paused", "value": "fastly.amazee.io/backup-paused", "operator": "source"},
					  {"name": "fastly.amazee.io/watch", "value": "fastly.amazee.io/backup-watch", "operator": "source"}
					]
				}
			]`,
			expect: map[string]interface{}{
				"fastly.amazee.io/paused":                           "false",
				"fastly.amazee.io/watch":                            "true",
				"fastly.amazee.io/backup-paused":                    nil,
				"fastly.amazee.io/backup-watch":                     nil,
				"fastly.amazee.io/delete-external-resources":        nil,
				"fastly.amazee.io/backup-delete-external-resources": nil,
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(tt *testing.T) {
			preAnnos, err := ProcessAnnotationRules(tc.rules, tc.input)
			if err != nil {
				tt.Fatal(err)
			}
			// fmt.Println(preAnnos)
			// fmt.Println(tc.expect)
			if !reflect.DeepEqual(preAnnos, tc.expect) {
				tt.Fatalf("Does not equal expected")
			}
		})
	}
}
