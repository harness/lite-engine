// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package schema

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

func bindata_read(data []byte, name string) ([]byte, error) { //nolint:revive,stylecheck
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz) //nolint:gosec
	gz.Close()

	if err != nil {
		return nil, fmt.Errorf("read %q: %v", name, err)
	}

	return buf.Bytes(), nil
}

var _callgraph_avsc = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xe4\x53\x31\x6e\xc3\x30\x0c\xdc\xf3\x0a\x81\x73\x90\x07\x78\xed\xd4\xad\xc8\x5a\x64\x60\x6d\x26\x26\x2a\x4b\x86\xa8\xb4\x30\x8c\xfc\xbd\x90\x1d\xa5\x96\xed\x21\x50\xd0\x0e\xad\x26\x89\xe2\xdd\x89\x47\xb1\xdf\x28\xa5\x14\x18\x6c\x08\x0a\x05\x2f\xe8\xc8\x78\xd8\x8e\x51\xdf\xb5\x04\x05\x38\x2a\xad\xab\x62\x30\xa4\x4a\x8b\x25\x81\x2a\x14\x78\xde\xd5\xe8\x0c\x89\xec\xd8\xc6\x94\x23\x93\xae\x04\x8a\xd7\xe1\x18\x56\x7f\xdb\x4d\xe4\xc0\xd8\x8a\xe4\x0a\xba\xdd\x8d\xa2\x29\x60\x72\xa1\x00\x9d\xc3\x0e\xb6\x4a\x2d\x53\xd8\x53\x23\x6b\xe0\xa9\xea\x53\xcd\xba\x9a\xa9\xce\x44\xd2\x92\x17\x49\xcb\xfa\xe6\xab\x1f\xc5\x82\x43\x0d\xf9\xda\x56\xb0\xbd\x72\x87\x90\x78\xc7\xe6\x04\x97\x75\xfa\x19\xbe\xc5\xf2\x1d\x4f\x94\x4f\xc0\xa9\x38\x1b\x7f\x27\xb0\xd4\x28\xf2\x9c\x8b\x6e\xd1\x61\x23\xf9\xcf\x1e\xd4\xf3\xe1\x03\x2a\x0b\x1d\xb4\x51\x6b\xd9\xd3\x51\x53\xe9\xd9\x9a\x84\xe7\xcd\x5a\x4d\x68\xee\x23\x42\xfd\x89\x9d\xec\xcf\x0f\x50\x1c\x59\xaf\x17\xb2\x8a\x3d\x2c\xa2\x69\xde\xf7\x69\x22\xbe\x3e\x9d\x9e\xc4\xef\x49\x63\x70\xe0\xcf\x4f\xa9\xd8\xb3\x2b\x29\xf3\xab\x07\xa7\x92\xaf\xda\xc7\x17\x46\x17\x62\xcd\x23\xe9\x8f\xb7\xee\x83\xe5\xe4\xb0\xad\x1f\x6e\xdf\x3f\x68\x5e\x45\xe2\xd9\x44\x9b\x7e\xaf\x87\xc3\xee\xa0\x36\x97\xcd\x57\x00\x00\x00\xff\xff\x69\x1b\xdf\x8a\x84\x07\x00\x00") //nolint:lll,revive,stylecheck

func callgraph_avsc() ([]byte, error) { //nolint:revive,stylecheck
	return bindata_read(
		_callgraph_avsc,
		"callgraph.avsc",
	)
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		return f()
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() ([]byte, error){
	"callgraph.avsc": callgraph_avsc,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
// data/
// -foo.txt
// -img/
// --a.png
// --b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if name == "" {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for name := range node.Children {
		rv = append(rv, name)
	}
	return rv, nil
}

type _bintree_t struct { //nolint:revive,stylecheck
	Func     func() ([]byte, error)
	Children map[string]*_bintree_t
}

var _bintree = &_bintree_t{nil, map[string]*_bintree_t{
	"callgraph.avsc": {callgraph_avsc, map[string]*_bintree_t{}},
}}
