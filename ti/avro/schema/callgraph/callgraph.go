// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Code generated for package schema by go-bindata DO NOT EDIT. (@generated)
// sources:
// callgraph.avsc
package schema

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _callgraphAvsc = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xe4\x53\xbb\x6e\xc3\x30\x0c\xdc\xf3\x15\x02\x67\x23\x1f\xe0\xb5\x40\x81\x6e\x45\xd6\x22\x03\x6b\xd1\xb1\x50\x5a\x32\x44\xa5\x45\x60\xe4\xdf\x0b\xd9\x71\xe2\xd7\x10\x28\x68\x87\xd6\x93\x44\xf1\xee\x48\x9e\xd9\x6e\x94\x52\x0a\x2c\xd6\x04\xb9\x82\x57\xf4\x64\x03\x64\x7d\x34\x9c\x1a\x82\x1c\x3c\x15\xce\xeb\x21\x18\x53\xa5\xc1\x82\x40\xe5\x0a\x82\xd9\x56\xe8\x2d\x89\x6c\x8d\x1b\x52\x4a\x43\xac\x05\xf2\xb7\xee\x1a\xbf\xf6\x7a\x1a\xc9\x81\x75\x9a\xe4\x02\xba\xbe\xf5\xa2\x53\xc0\xe8\x41\x01\x7a\x8f\x27\xc8\x94\x5a\xa6\x98\x40\xb5\xac\x81\xc7\xaa\x4f\x95\x61\x3d\x53\x9d\x89\x4c\x5b\x5e\x24\x2d\xfb\x9b\x7f\x6d\x2f\x16\x27\x54\x53\xa8\x9c\x86\xec\xc2\x1d\x43\x12\xbc\xb1\x07\x38\xaf\xd3\xcf\xf0\x0d\x16\x1f\x78\xa0\x74\x02\x33\x15\x37\x36\xdc\x09\x2c\x18\x45\x5e\x52\xd1\x0d\x7a\xac\x25\xbd\xec\x4e\x3d\x1d\xde\xa1\x92\xd0\x51\x1b\x99\x65\x47\x25\x53\x11\x8c\xb3\x13\x9e\x77\xe7\x98\xd0\xde\x47\x84\xfc\x85\x27\xd9\x1d\x1f\xa0\x28\x0d\xa7\x37\x52\xa1\x3c\xa3\x61\xba\x99\x38\x92\xcf\x14\x68\x2a\xf1\xc8\x01\x72\x55\x22\x0b\x9d\x57\x29\xf7\x8b\xe8\x34\xef\x76\x1b\xd5\xb4\xbe\xf1\x81\x24\xec\x88\x31\x4e\xf5\xcf\x6f\xbe\xb8\xa3\x2f\x28\x71\x7d\xe2\xa4\x26\xbf\x7f\x3b\x54\x38\x4c\x61\xe8\xb9\x27\xfd\x71\xeb\x3e\x8d\x1c\x3c\x36\xd5\xc3\xf6\xfd\x03\xf3\x34\x49\x30\x76\x18\xd3\xef\x79\xd8\x9d\xf6\x6a\x73\xde\x7c\x07\x00\x00\xff\xff\x0f\x19\x2b\x11\xd8\x07\x00\x00")

func callgraphAvscBytes() ([]byte, error) {
	return bindataRead(
		_callgraphAvsc,
		"callgraph.avsc",
	)
}

func callgraphAvsc() (*asset, error) {
	bytes, err := callgraphAvscBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "callgraph.avsc", size: 2008, mode: os.FileMode(420), modTime: time.Unix(1747912079, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
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
var _bindata = map[string]func() (*asset, error){
	"callgraph.avsc": callgraphAvsc,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
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
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"callgraph.avsc": &bintree{callgraphAvsc, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
