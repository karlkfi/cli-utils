// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package kyq

import (
	"bufio"
	"bytes"
	"container/list"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"k8s.io/klog/v2"

	// Using gopkg.in/yaml.v3 instead of sigs.k8s.io/yaml on purpose.
	// yaml.v3 correctly parses ints:
	// https://github.com/kubernetes-sigs/yaml/issues/45
	// yaml.v3 Node is also used as input to yqlib.
	"gopkg.in/yaml.v3"
)

// Get evaluates the yq expression to extract a value from the input map.
// For details about the yq expression language, see: https://mikefarah.gitbook.io/yq/
func Get(obj map[string]interface{}, expression string) (interface{}, bool, error) {
	// format input object as yaml for input into yq
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return nil, false, err
	}

	// evaluate expression using yq
	yamlBytes, found, err := evaluateYaml(yamlBytes, expression)
	if err != nil {
		return nil, false, err
	}
	if !found {
		// null result
		return nil, false, nil
	}

	// parse yaml result
	var result interface{}
	err = yaml.Unmarshal(yamlBytes, &result)
	if err != nil {
		return nil, false, err
	}

	return result, true, nil
}

// Set evaluates the yq expression to set a value in the input map.
// For details about the yq expression language, see: https://mikefarah.gitbook.io/yq/
func Set(obj map[string]interface{}, expression string, value interface{}) error {
	// format input object as yaml for input into yq
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}

	// inject value into expression as json (wrapped scalers, etc.)
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	// https://mikefarah.gitbook.io/yq/operators/assign-update#update-selected-results
	expression = fmt.Sprintf("(%s) = %s", expression, jsonBytes)

	// evaluate expression using yq
	yamlBytes, found, err := evaluateYaml(yamlBytes, expression)
	if err != nil {
		return err
	}
	if !found {
		// TODO: error if no result?
		return nil
	}

	// parse yaml result object and update the input object in-place
	err = yaml.Unmarshal(yamlBytes, &obj)
	if err != nil {
		return err
	}

	return nil
}

// evaluateYaml wraps evaluateStream to simplify processing of yaml byte arrays.
func evaluateYaml(yamlBytes []byte, expression string) ([]byte, bool, error) {
	klog.V(4).Infof("kyq expression: %s", expression)

	// stream input from a byte array
	reader := bytes.NewReader(yamlBytes)

	// stream output to a byte array
	var output bytes.Buffer
	printer := writerToYamlPrinter(bufio.NewWriter(&output))

	// Parser construction isn't especially expensive compared to going to yaml
	// and back, so we don't need to re-use the parser.
	treeCreator := yqlib.NewExpressionParser()

	// Hypothetically you could use the same expression on multiple inputs, but
	// that would make the usage of this package more complicated, and we don't
	// need it for apply-time-mutation. So just parse every time.
	node, err := treeCreator.ParseExpression(expression)
	if err != nil {
		return nil, false, err
	}

	// Only one file, so the file name used in errors isn't very important.
	filename := "object"

	// dooo eeet!
	found, err := evaluateStream(filename, reader, node, printer)
	if err != nil {
		return output.Bytes(), found, err
	}

	return output.Bytes(), found, nil
}

func writerToYamlPrinter(writer io.Writer) yqlib.Printer {
	unwrapScalar := false
	colors := false
	indent := 2
	separators := true
	return yqlib.NewPrinter(writer, yqlib.YamlOutputFormat, unwrapScalar, colors, indent, separators)
}

// evaluateStream decodes a stream of yaml input, gets or sets a node, and
// prints the result as yaml. This simplified implementation expects a single
// input yaml object, and will error otherwise. Returns false if there are no
// result nodes.
func evaluateStream(filename string, reader io.Reader, exNode *yqlib.ExpressionNode, printer yqlib.Printer) (bool, error) {
	// parse yaml input to a node tree
	decoder := yaml.NewDecoder(reader)
	var input yaml.Node
	err := decoder.Decode(&input)
	if err != nil {
		return false, err
	}

	// Wrap the input node tree with some file context.
	// This is needlessly complicated because yq accepts multiple input files.
	inputList := list.New()
	inputList.PushBack(&yqlib.CandidateNode{
		Filename: filename,
		Node:     &input,
	})
	inputContext := yqlib.Context{MatchingNodes: inputList}

	// Navigator construction isn't especially expensive compared to going to
	// yaml and back, so we don't need to re-use the navigator.
	treeNavigator := yqlib.NewDataTreeNavigator()

	// dooo eeet!
	result, err := treeNavigator.GetMatchingNodes(inputContext, exNode)
	if err != nil {
		return false, err
	}

	found := (result.MatchingNodes.Len() > 0)
	if !found {
		return false, nil
	}

	// print the matching node(s) as yaml
	err = printer.PrintResults(result.MatchingNodes)
	if err != nil {
		return found, err
	}
	return found, nil
}
