// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package java

import (
	"context"
	"testing"

	"github.com/harness/lite-engine/internal/filesystem"
	"github.com/harness/lite-engine/ti/instrumentation/common"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

const (
	pkgDetectTestdata = "testdata/pkg_detection"
)

func TestDetectJavaPkgs(t *testing.T) {
	ctrl, _ := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()

	log := logrus.New()

	l, err := DetectPkgs(pkgDetectTestdata, log, filesystem.New())
	assert.NotContains(t, l, "com.google.test.test")
	assert.Contains(t, l, "xyz")
	assert.Contains(t, l, "test1.test1")
	assert.Len(t, l, 2)
	assert.Nil(t, err)
}

func Test_ParseJavaNode(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		node     common.Node
	}{
		{
			name:     "ParseJavaNode_JavaSourceFile",
			filename: "320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.java",
			node: common.Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtils",
				Type:  common.NodeType_SOURCE,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_JavaTestFile",
			filename: "320-ci-execution/src/test/java/io/harness/stateutils/buildstate/ConnectorUtilsTest.java",
			node: common.Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  common.NodeType_TEST,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_JavaResourceFile",
			filename: "320-ci-execution/src/test/resources/all.json",
			node: common.Node{
				Type: common.NodeType_RESOURCE,
				Lang: common.LangType_JAVA,
				File: "all.json",
			},
		},
		{
			name:     "ParseJavaNode_ScalaSourceFile",
			filename: "320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.scala",
			node: common.Node{
				Class: "ConnectorUtils",
				Type:  common.NodeType_SOURCE,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_ScalaTestFile_ScalaTestPath",
			filename: "320-ci-execution/src/test/scala/io/harness/stateutils/buildstate/ConnectorUtilsTest.scala",
			node: common.Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  common.NodeType_TEST,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_ScalaTestFile_JavaTestPath",
			filename: "320-ci-execution/src/test/java/io/harness/stateutils/buildstate/ConnectorUtilsTest.scala",
			node: common.Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  common.NodeType_TEST,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_KotlinSourceFile",
			filename: "320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.kt",
			node: common.Node{
				Class: "ConnectorUtils",
				Type:  common.NodeType_SOURCE,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_KotlinTestFile_KotlinTestPath",
			filename: "320-ci-execution/src/test/kotlin/io/harness/stateutils/buildstate/ConnectorUtilsTest.kt",
			node: common.Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  common.NodeType_TEST,
				Lang:  common.LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_KotlinTestFile_JavaTestPath",
			filename: "320-ci-execution/src/test/java/io/harness/stateutils/buildstate/ConnectorUtilsTest.kt",
			node: common.Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  common.NodeType_TEST,
				Lang:  common.LangType_JAVA,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, _ := ParseJavaNode(tt.filename, []string{})
			assert.Equal(t, tt.node, *n, "extracted java node does not match")
		})
	}
}
