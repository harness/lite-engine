// Code generated for package main by go-bindata DO NOT EDIT. (@generated)
// sources:
// callgraph_1_1.avsc
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

var _callgraph_1_1Avsc = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xe4\x53\x4d\x6b\xc3\x30\x0c\xbd\xf7\x57\x18\x5d\x76\x09\xfd\x01\xb9\x0e\x06\xbb\x8d\x5e\x47\x0f\x5a\xac\x34\x66\x8a\x1d\x2c\x77\xa3\x84\xfe\xf7\xe1\xa4\x69\xf3\x35\x08\x29\xdb\x61\xf3\xc9\x96\xf5\xde\xb3\xf4\xac\x7a\xa3\x94\x52\x60\xb1\x24\x48\x15\xbc\xa0\x27\x1b\x20\x69\xa3\xe1\x54\x11\xa4\xe0\x29\x73\x5e\x77\xc1\x98\x2a\x15\x66\x04\x2a\x55\x10\xcc\xb6\x40\x6f\x49\x64\x6b\x5c\x97\x92\x1b\x62\x2d\x90\xbe\x36\xc7\xb8\xea\xeb\xae\x27\x07\xd6\x69\x92\x0b\xe8\x7a\xd7\x8a\x0e\x01\xbd\x0b\x05\xe8\x3d\x9e\x20\x51\x6a\x9a\x62\x02\x95\x32\x07\xee\xab\x3e\x16\x86\xf5\x48\x75\x24\x32\x2c\x79\x92\x34\xad\x6f\xbc\xea\x56\x2c\x76\xa8\xa4\x50\x38\x0d\xc9\x85\x3b\x86\x24\x78\x63\x0f\x70\x9e\xa7\x1f\xe1\x2b\xcc\xde\xf1\x40\xeb\x09\xcc\x50\xdc\xd8\xb0\x10\x98\x31\x8a\x3c\xaf\x45\x57\xe8\xb1\x94\xf5\xcf\x6e\xd4\xd7\xc3\x1b\xd4\x2a\x74\xd4\x46\x66\xd9\x51\xce\x94\x05\xe3\xec\x80\xe7\xcd\x39\x26\xb4\xcb\x88\x90\x3f\xf1\x24\xbb\xe3\x1d\x14\xb9\xe1\x6f\x0a\x79\x58\x80\x2e\x50\x9e\xd0\x30\xdd\x5c\xec\xe9\x27\x0a\x34\xe5\x78\xe4\x00\xa9\xca\x91\x85\xce\xb3\x94\xfb\x49\x74\x98\x77\x3b\xf5\x2a\x9a\x1f\xf9\x40\x12\x76\xc4\x18\xdb\xfa\xe7\x47\x5f\xdc\xd1\x67\xb4\x72\x7e\x62\xa7\x06\xff\xbf\xee\x5e\xd8\x75\xa1\xab\xb9\x25\xfd\x71\xeb\x3e\x8c\x1c\x3c\x56\xc5\xdd\xf6\xfd\x03\xf3\x34\x49\x30\xb6\x6b\xd3\xef\x79\xd8\xec\xf6\x6a\x73\xde\x7c\x05\x00\x00\xff\xff\x47\x9c\xd6\xf2\xd9\x07\x00\x00")

func callgraph_1_1AvscBytes() ([]byte, error) {
	return bindataRead(
		_callgraph_1_1Avsc,
		"callgraph_1_1.avsc",
	)
}

func callgraph_1_1Avsc() (*asset, error) {
	bytes, err := callgraph_1_1AvscBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "callgraph_1_1.avsc", size: 2009, mode: os.FileMode(420), modTime: time.Unix(1753860849, 0)}
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
	"callgraph_1_1.avsc": callgraph_1_1Avsc,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//
//	data/
//	  foo.txt
//	  img/
//	    a.png
//	    b.png
//
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
	"callgraph_1_1.avsc": &bintree{callgraph_1_1Avsc, map[string]*bintree{}},
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
