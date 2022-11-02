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

func Test_ParseJavaNode(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		node     Node
	}{
		{
			name:     "ParseJavaNode_JavaSourceFile",
			filename: "320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.java",
			node: Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtils",
				Type:  NodeType_SOURCE,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_JavaTestFile",
			filename: "320-ci-execution/src/test/java/io/harness/stateutils/buildstate/ConnectorUtilsTest.java",
			node: Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  NodeType_TEST,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_JavaResourceFile",
			filename: "320-ci-execution/src/test/resources/all.json",
			node: Node{
				Type: NodeType_RESOURCE,
				Lang: LangType_JAVA,
				File: "all.json",
			},
		},
		{
			name:     "ParseJavaNode_ScalaSourceFile",
			filename: "320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.scala",
			node: Node{
				Class: "ConnectorUtils",
				Type:  NodeType_SOURCE,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_ScalaTestFile_ScalaTestPath",
			filename: "320-ci-execution/src/test/scala/io/harness/stateutils/buildstate/ConnectorUtilsTest.scala",
			node: Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  NodeType_TEST,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_ScalaTestFile_JavaTestPath",
			filename: "320-ci-execution/src/test/java/io/harness/stateutils/buildstate/ConnectorUtilsTest.scala",
			node: Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  NodeType_TEST,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_KotlinSourceFile",
			filename: "320-ci-execution/src/main/java/io/harness/stateutils/buildstate/ConnectorUtils.kt",
			node: Node{
				Class: "ConnectorUtils",
				Type:  NodeType_SOURCE,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_KotlinTestFile_KotlinTestPath",
			filename: "320-ci-execution/src/test/kotlin/io/harness/stateutils/buildstate/ConnectorUtilsTest.kt",
			node: Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  NodeType_TEST,
				Lang:  LangType_JAVA,
			},
		},
		{
			name:     "ParseJavaNode_KotlinTestFile_JavaTestPath",
			filename: "320-ci-execution/src/test/java/io/harness/stateutils/buildstate/ConnectorUtilsTest.kt",
			node: Node{
				Pkg:   "io.harness.stateutils.buildstate",
				Class: "ConnectorUtilsTest",
				Type:  NodeType_TEST,
				Lang:  LangType_JAVA,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, _ := ParseJavaNode(tt.filename)
			assert.Equal(t, tt.node, *n, "extracted java node does not match")
		})
	}
}
