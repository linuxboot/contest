// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package pluginregistry

import (
	"fmt"

	"github.com/linuxboot/contest/pkg/test"
)

type ErrStepLabelIsMandatory struct {
	TestStepDescriptor test.TestStepDescriptor
}

func (err ErrStepLabelIsMandatory) Error() string {
	return fmt.Sprintf("step has no label, but it is mandatory (step: %+v)", err.TestStepDescriptor)
}

// ErrInvalidStepLabelFormat tells that a variable name doesn't fit the variable name format (alphanum + '_')
type ErrInvalidStepLabelFormat struct {
	InvalidName string
	Err         error
}

func (err ErrInvalidStepLabelFormat) Error() string {
	return fmt.Sprintf("'%s' doesn't match variable name format: %v", err.InvalidName, err.Err)
}

func (err ErrInvalidStepLabelFormat) Unwrap() error {
	return err.Err
}
