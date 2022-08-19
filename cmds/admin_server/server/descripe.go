package server

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	INT    = "int"
	UINT   = "uint"
	STRING = "string"
	TIME   = "time"
	ENUM   = "enum"
)

type FieldMetaData struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type Description []FieldMetaData

/*
	DescribeEntity returns a Description for some entity fields
	to help frontend renders appropiate filters and tables.
	it depends on the tags assigned to the entity fields to create the needed metadata.
	it expects tags: filter, values
*/
func DescribeEntity(entity interface{}) (Description, error) {
	var describtion Description
	t := reflect.TypeOf(entity)

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous {
			return nil, fmt.Errorf("Error Anonymous Field")
		}

		name := sf.Tag.Get("json")
		tagValue := sf.Tag.Get("filter")
		var values []string
		if tagValue == ENUM {
			values = strings.Split(sf.Tag.Get("values"), ",")
		}
		describtion = append(describtion, FieldMetaData{
			Name:   name,
			Type:   tagValue,
			Values: values,
		})
	}

	return describtion, nil
}
