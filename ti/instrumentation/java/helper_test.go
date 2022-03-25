// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	pkgDetectTestdata = "testdata/pkg_detection"
)

func TestDetectJavaPkgs(t *testing.T) {
	ctrl, _ := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()

	l, err := DetectPkgs(pkgDetectTestdata, log, filesystem.New())
	assert.Contains(t, l, "com.google.test.test")
	assert.Contains(t, l, "xyz")
	assert.Contains(t, l, "test1.test1")
	assert.Len(t, l, 3)
	assert.Nil(t, err)
}
