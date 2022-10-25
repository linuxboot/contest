// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"fmt"
	"reflect"

	"github.com/linuxboot/contest/pkg/target"
)

type ParamExpander struct {
	t    *target.Target
	vars StepsVariablesReader
}

func NewParamExpander(target *target.Target, vars StepsVariablesReader) *ParamExpander {
	return &ParamExpander{t: target, vars: vars}
}

func (pe *ParamExpander) Expand(value string) (string, error) {
	p := NewParam(value)
	return p.Expand(pe.t, pe.vars)
}

func (pe *ParamExpander) ExpandObject(obj interface{}, out interface{}) error {
	if reflect.PtrTo(reflect.TypeOf(obj)) != reflect.TypeOf(out) {
		return fmt.Errorf("object types differ")
	}

	var visit func(vin reflect.Value, vout reflect.Value) error
	visit = func(vin reflect.Value, vout reflect.Value) error {
		if !vout.CanSet() {
			return fmt.Errorf("readonly field: %v", vout.Type().Name())
		}

		switch vin.Kind() {
		case reflect.String:
			// this is the only thing we actually expand in the obj tree
			val, err := pe.Expand(vin.String())
			if err != nil {
				return err
			}
			vout.SetString(val)

		case reflect.Pointer:
			if vin.IsNil() {
				// input ptr is nil, just set the same on out
				vout.Set(vin)
				break
			}

			val := reflect.New(vin.Type().Elem())
			if err := visit(vin.Elem(), val.Elem()); err != nil {
				return err
			}
			vout.Set(val)

		case reflect.Interface:
			if vin.IsNil() {
				// interface holds a nil (typed or untyped), set nil on out
				vout.Set(vin)
				break
			}

			// make a new type of interface impl and write into it
			val := reflect.New(vin.Elem().Type())
			if err := visit(vin.Elem(), val.Elem()); err != nil {
				return err
			}
			vout.Set(val)

		case reflect.Map:
			val := reflect.MakeMap(vin.Type())
			iter := vin.MapRange()
			for iter.Next() {
				ikey := iter.Key()
				ival := iter.Value()

				// expand map keys as well
				nkey := reflect.New(ikey.Type())
				if err := visit(ikey, nkey.Elem()); err != nil {
					return err
				}

				nval := reflect.New(ival.Type())
				if err := visit(ival, nval.Elem()); err != nil {
					return err
				}

				val.SetMapIndex(nkey.Elem(), nval.Elem())
			}
			vout.Set(val)

		case reflect.Struct:
			for i := 0; i < vin.NumField(); i++ {
				if err := visit(vin.Field(i), vout.Field(i)); err != nil {
					return err
				}
			}

		case reflect.Array:
			val := reflect.New(vin.Type())
			for i := 0; i < vin.Len(); i++ {
				if err := visit(vin.Index(i), val.Elem().Index(i)); err != nil {
					return err
				}
			}
			vout.Set(val.Elem())

		case reflect.Slice:
			elemType := vin.Type().Elem()
			len := vin.Len()

			val := reflect.MakeSlice(reflect.SliceOf(elemType), len, len)
			for i := 0; i < len; i++ {
				if err := visit(vin.Index(i), val.Index(i)); err != nil {
					return err
				}
			}
			vout.Set(val)

		default:
			vout.Set(vin)
		}
		return nil
	}

	vin := reflect.ValueOf(obj)
	vout := reflect.ValueOf(out)
	if vout.Kind() != reflect.Ptr {
		return fmt.Errorf("out must be a pointer type")
	}

	return visit(vin, reflect.Indirect(vout))
}
